package main

import (
	"github.com/spf13/cobra"
)

// declare it globally so the sibling files can see it and attach to it
var searchCmd = &cobra.Command{
	Use:   "search",
	Short: "Query USGS metadata registries",
	Long:  `Search workflows for discovering collection datasets and scene records.`,
}

func init() {
	// add search to the root application command
	rootCmd.AddCommand(searchCmd)
}
