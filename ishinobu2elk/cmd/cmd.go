package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	upDir  string
	upFile string
)

func Execute() {
	var rootCmd = &cobra.Command{
		Use:   "ishinobu2elk",
		Short: "ishinobu2elk is a tool to load logs collected by ishinobu into the ELK stack",
	}

	var up = &cobra.Command{
		Use:   "load",
		Short: "Starts Docker containers and loads data into ELK stack",
		Run: func(cmd *cobra.Command, args []string) {
			readFiles(upDir, upFile)
			runDockerCompose()
		},
	}

	up.Flags().StringVar(&upDir, "dir", "", "Directoy where compressed JSON logs (tar.gz) are stored")
	up.Flags().StringVar(&upFile, "file", "", "tar.gz where compressed JSON logs are stored")

	var down = &cobra.Command{
		Use:   "down",
		Short: "Stops Docker containers",
		Run: func(cmd *cobra.Command, args []string) {
			stopDockerCompose()
		},
	}

	var clean = &cobra.Command{
		Use:   "clean",
		Short: "Cleans up the data directory, stops Docker containers and removes the data volume",
		Run: func(cmd *cobra.Command, args []string) {
			cleanDockerCompose()
		},
	}

	rootCmd.AddCommand(up, down, clean)

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
