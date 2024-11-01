package cmd

import (
	"archive/tar"
	"compress/gzip"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Configuration constants
const (
	OutputDir         = "./.resources/elk/json_logs"
	ConcurrentWorkers = 4 // Adjust based on CPU cores
)

//go:embed resources/*
var resources embed.FS

func readFiles(dir string, file string) {
	log.Printf("dir: %d file:%d", len(dir), len(file))
	// Initialize logger
	logFile, err := os.OpenFile("load_ishinobu.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}
	defer logFile.Close()
	log.SetOutput(io.MultiWriter(os.Stdout, logFile))
	log.Println("Copying JSON files...")

	if (dir == "" && file == "") || (dir != "" && file != "") {
		log.Fatal("You must specify exactly one of --dir or --file")
	}

	// Ensure processed directories exist
	dirs := []string{OutputDir}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			log.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	var archives []string
	if dir != "" {
		// Find all tar.gz files in SourceDir
		archives, err = filepath.Glob(filepath.Join(dir, "*.tar.gz"))
		if err != nil {
			log.Fatalf("Error finding tar.gz files: %v", err)
		}

		if len(archives) == 0 {
			log.Println("No tar.gz files found to process.")
			return
		}

		log.Printf("Found %d archive(s) to process.\n", len(archives))
	} else if file != "" {
		archives = append(archives, file)
	}

	// Set up worker pool
	archiveChan := make(chan string, len(archives))
	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < ConcurrentWorkers; i++ {
		wg.Add(1)
		go worker(&wg, archiveChan)
	}

	// Send archives to channel
	for _, archive := range archives {
		log.Println(archive)
		archiveChan <- archive
	}
	close(archiveChan)

	// Wait for all workers to finish
	wg.Wait()
}

func runDockerCompose() {
	// Ensure the target directory exists
	if _, err := os.Stat("./.resources/elk"); os.IsNotExist(err) {
		log.Fatalf("Target directory does not exist: %s", "./.resources/elk")
	}

	err := copyEmbeddedFiles("resources/elk", "./.resources/elk")
	if err != nil {
		log.Fatalf("Failed to copy embedded files: %v", err)
	}

	cmd := exec.Command("docker", "compose", "-f", "./.resources/elk/docker-compose.yml", "up", "-d")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		log.Fatalf("Failed to run docker compose up: %v", err)
	}
}

func stopDockerCompose() {
	cmd := exec.Command("docker", "compose", "-f", "./.resources/elk/docker-compose.yml", "down")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		log.Fatalf("Failed to run docker-compose down: %v", err)
	}
}

func cleanDockerCompose() {
	cmd := exec.Command("docker", "compose", "-f", "./.resources/elk/docker-compose.yml", "down", "-v")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		log.Fatalf("Failed to run docker compose down -v: %v", err)
	}

	cmd = exec.Command("rm", "-rf", "./.resources", "load_ishinobu.log")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		log.Fatalf("Failed to clean up directories: %v", err)
	}

}

// copyEmbeddedFiles copies files from the embedded filesystem to the target directory
func copyEmbeddedFiles(srcDir, destDir string) error {
	// Walk through the embedded files
	return fs.WalkDir(resources, srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Compute the relative path
		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}

		// Determine the target path
		targetPath := filepath.Join(destDir, relPath)

		if d.IsDir() {
			// Create the directory
			return os.MkdirAll(targetPath, os.ModePerm)
		}

		// Read the file from the embedded filesystem
		data, err := resources.ReadFile(path)
		if err != nil {
			return err
		}

		// Write the file to the target directory
		return ioutil.WriteFile(targetPath, data, 0644)
	})
}

func worker(wg *sync.WaitGroup, archives <-chan string) {
	defer wg.Done()
	for archive := range archives {
		log.Printf("Processing archive: %s\n", archive)
		err := processArchive(archive)
		if err != nil {
			log.Printf("Error processing archive %s: %v\n", archive, err)
			continue
		}
		log.Printf("Successfully processed archive: %s\n", archive)
	}
}

func processArchive(archivePath string) error {
	// Open the tar.gz file
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open archive: %w", err)
	}
	defer file.Close()

	// Create a gzip reader
	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzReader.Close()

	// Create a tar reader
	tarReader := tar.NewReader(gzReader)

	// Iterate through the files in the archive
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break // End of archive
		}
		if err != nil {
			log.Printf("Error reading tar archive %s: %v", archivePath, err)
			continue
		}

		// Skip files starting with ._
		baseName := filepath.Base(header.Name)
		if strings.HasPrefix(baseName, "._") {
			continue
		}

		// Only process regular files with .json extension
		if header.Typeflag != tar.TypeReg || !strings.HasSuffix(header.Name, ".json") {
			continue
		}

		// Define the path to copy the JSON file
		jsonPath := filepath.Join(OutputDir, baseName)

		// Create the JSON file
		outFile, err := os.Create(jsonPath)
		if err != nil {
			log.Printf("Error creating JSON file %s: %v", jsonPath, err)
			continue
		}

		// Copy the file content
		if _, err := io.Copy(outFile, tarReader); err != nil {
			log.Printf("Error extracting JSON file %s: %v", jsonPath, err)
			outFile.Close()
			continue
		}
		outFile.Close()
	}

	return nil
}
