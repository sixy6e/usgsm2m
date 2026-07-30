package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"

	"github.com/sixy6e/usgsm2m/pkg/usgsm2m"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	filePath        string
	downloadSysFlag string
)

var downloadCmd = &cobra.Command{
	Use:   "download [scene IDs...]",
	Short: "Download scenes from USGS M2M using a generic dataset catalog",
	Args:  cobra.MinimumNArgs(0), // 0 since -f flag can fulfill args requirement
	RunE: func(cmd *cobra.Command, args []string) error {
		// if a file path is provided, read lines and append them to args
		if filePath != "" {
			file, err := os.Open(filePath)
			if err != nil {
				return fmt.Errorf("failed to open scene list file: %w", err)
			}
			defer file.Close()

			// handler for Windows [:(] generated UTF-16 text files
			decoder := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewDecoder()
			utf8Reader := transform.NewReader(file, decoder)

			scanner := bufio.NewScanner(utf8Reader)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				// skip empty lines or commented out lines (using #)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				args = append(args, line)
			}
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("error reading scene list file: %w", err)
			}
		}

		// gatekeeper check: make sure we got IDs from *somewhere*
		if len(args) == 0 {
			return errors.New("you must provide at least one scene ID as an argument or specify a file using the -f flag")
		}

		// sanity check for required authentication fields
		if cfg.Auth.Username == "" || cfg.Auth.Token == "" {
			return errors.New("missing authentication credentials; please set username and token in your .m2m.toml or use --username/--token flags")
		}

		// intercept Ctrl+C (SIGINT) and SIGTERM
		// wrap Cobra's context so any upstream context values remain intact.
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop() // clean up the signal listener registration on exit

		dataset := viper.GetString("defaults.dataset")
		concurrency := viper.GetInt("defaults.concurrency")
		outdir := viper.GetString("defaults.output_dir")

		logger.Info("Initializing USGS M2M Client Pool", "user", cfg.Auth.Username)

		// instantiate the Client using user's signature
		client, err := usgsm2m.NewClient(
			cfg.Auth.Username,
			cfg.Auth.Token,
			concurrency,
			outdir,
			usgsm2m.WithLogger(logger),
		)
		if err != nil {
			return fmt.Errorf("failed to initialise client: %w", err)
		}

		// authenticate
		logger.Info("Logging into USGS M2M service...")
		if err := client.Login(ctx); err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		logger.Info("Login successful.")

		defer func() {
			if err := client.Logout(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "Error cleaning up session: %v\n", err)
			}
		}()

		// validate and stage the scene list
		// generate a unique batch label for this run
		// (avoid collisions with previous requests)
		batchLabel := usgsm2m.GenerateBatchId()

		logger.Info("Validating scene list with USGS", "dataset", dataset, "count", len(args))
		confirmed := client.Request.AddToSceneListSafely(ctx, dataset, batchLabel, args)
		if len(confirmed) == 0 {
			return errors.New("no scene IDs were successfully validated by USGS")
		}

		// fetch product download options
		options, err := client.Request.GetDownloadOptions(ctx, dataset, confirmed)
		if err != nil {
			return fmt.Errorf("failed to fetch product options: %w", err)
		}

		// filter and resolve direct download URLs
		// items := client.Request.FilterForZip(options)
		items := usgsm2m.FilterBySystem(options, downloadSysFlag)
		links, err := client.Request.GetDownloadURLs(ctx, items)
		if err != nil {
			return fmt.Errorf("failed to retrieve download links: %w", err)
		}

		// start the concurrent download engine
		logger.Info("Starting download pool", "jobs", len(links))

		for entityId, url := range links {
			// check if user hit Ctrl+C before enqueuing the next job
			select {
			case <-ctx.Done():
				logger.Warn("Shutdown requested; stopping job dispatch")
				goto WaitBlock // jump out of the loop to the Wait() call
			default:
				// build the download job
				job := usgsm2m.DownloadJob{
					EntityId: entityId,
					URL:      url,
				}
				// pass the signaled context down into the workers
				client.Downloader.Enqueue(ctx, job)
			}
		}

	WaitBlock:
		// wait for completion or active worker cancellations
		client.Downloader.Wait()
		logger.Info("Batch process execution finished")

		return nil
	},
}

func init() {
	rootCmd.AddCommand(downloadCmd)

	// local file input
	downloadCmd.Flags().StringVarP(&filePath, "file", "f", "", "Text file containing scene IDs (one per line)")

	downloadCmd.Flags().StringP("dataset", "d", "landsat_ot_c2_l1", "The USGS dataset name")
	downloadCmd.Flags().IntP("concurrency", "c", 4, "Number of concurrent downloads")
	downloadCmd.Flags().StringP("output", "o", "./downloads", "Output directory for downloaded files")
	downloadCmd.Flags().StringVar(&downloadSysFlag, "sys", "", "Target M2M download system code (e.g., 'ls_zip', 'dds')")

	viper.BindPFlag("defaults.dataset", downloadCmd.Flags().Lookup("dataset"))
	viper.BindPFlag("defaults.concurrency", downloadCmd.Flags().Lookup("concurrency"))
	viper.BindPFlag("defaults.output_dir", downloadCmd.Flags().Lookup("output"))
}
