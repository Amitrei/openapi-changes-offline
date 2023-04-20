// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package cmd

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/pb33f/libopenapi/what-changed/reports"
	"github.com/pb33f/openapi-changes/git"
	"github.com/pb33f/openapi-changes/model"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
	"github.com/twinj/uuid"
)

func GetSummaryCommand() *cobra.Command {

	cmd := &cobra.Command{
		SilenceUsage:  true,
		SilenceErrors: false,
		Use:           "summary",
		Short:         "See a summary of changes",
		Long:          "print a summary of what changed, view a simple tree of changes and summary",
		Example:       "openapi-changes summary /path/to/git/repo path/to/file/in/repo/openapi.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {

			updateChan := make(chan *model.ProgressUpdate)
			errorChan := make(chan model.ProgressError)
			doneChan := make(chan bool)
			failed := false
			latestFlag, _ := cmd.Flags().GetBool("top")
			noColorFlag, _ := cmd.Flags().GetBool("no-color")
			limitFlag, _ := cmd.Flags().GetInt("limit")

			if noColorFlag {
				pterm.DisableStyling()
				pterm.DisableColor()
			}

			PrintBanner()

			// if there are no args, print out how to use the console.
			if len(args) == 0 {
				PrintHowToUse("summary")
				return nil
			}

			listenForUpdates := func(updateChan chan *model.ProgressUpdate, errorChan chan model.ProgressError) {
				var spinner *pterm.SpinnerPrinter
				if !noColorFlag {
					spinner, _ = pterm.DefaultSpinner.Start("starting work.")
				}
				for {
					select {
					case update, ok := <-updateChan:
						if ok {
							if !noColorFlag {
								spinner.UpdateText(update.Message)
							}
							if update.Warning {
								pterm.Warning.Println(update.Message)
							}
						} else {
							if !failed {
								if !noColorFlag {
									spinner.Info("printing summary")
								}
							} else {
								if !noColorFlag {
									spinner.Fail("failed to complete. sorry!")
								}
							}
							doneChan <- true
							return
						}
					case err := <-errorChan:
						failed = true
						if !noColorFlag {
							spinner.Fail(fmt.Sprintf("Stopped: %s", err.Message))
						}
						pterm.Println(err)
						pterm.Println()
						doneChan <- true
						return
					}
				}
			}

			// check for two args (left and right)
			if len(args) < 2 {

				// check if arg is an url (like a github url)
				url, err := url.Parse(args[0])
				if err == nil {

					if url.Host == "github.com" {
						go listenForUpdates(updateChan, errorChan)

						user, repo, filePath, err := ExtractGithubDetailsFromURL(url)
						if err != nil {
							errorChan <- model.ProgressError{
								Job:     "github url",
								Message: fmt.Sprintf("error extracting github details from url: %s", err.Error()),
							}
							<-doneChan
							return err
						}
						err = runGithubHistorySummary(user, repo, filePath, latestFlag, limitFlag, updateChan, errorChan)
						// wait for things to be completed.
						<-doneChan
						if err != nil {
							return err
						}
						return nil
					}

				} else {
					pterm.Error.Println("Two arguments are required to compare left and right OpenAPI Specifications.")
					return nil
				}
			}
			if len(args) == 2 {

				// check if the first arg is a directory, if so - process as a git history operation.
				p := args[0]
				f, err := os.Stat(p)
				if err != nil {
					pterm.Error.Printf("Cannot open file/repository: '%s'", args[0])
					return err
				}

				if f.IsDir() {

					repo := p
					p = args[1]
					f, err = os.Stat(filepath.Join(repo, p))
					if err != nil {
						pterm.Error.Printf("Cannot open file/repository: '%s'", args[1])
						return err
					}

					go listenForUpdates(updateChan, errorChan)

					err = runGitHistorySummary(args[0], args[1], latestFlag, updateChan, errorChan)

					<-doneChan

					if err != nil {
						pterm.Error.Println(err.Error())
						return err
					}
				} else {
					go listenForUpdates(updateChan, errorChan)
					errs := runLeftRightSummary(args[0], args[1], updateChan, errorChan)
					<-doneChan
					if len(errs) > 0 {
						for e := range errs {
							pterm.Error.Println(errs[e].Error())
						}
						return errors.New("unable to process specifications")
					}
					return nil
				}
			}
			pterm.Error.Println("too many arguments, expecting two (2)")
			return nil
		},
	}
	cmd.Flags().BoolP("no-color", "n", false, "Disable color and style output (very useful for CI/CD)")
	return cmd
}

func runLeftRightSummary(left, right string, updateChan chan *model.ProgressUpdate, errorChan chan model.ProgressError) []error {

	var leftBytes, rightBytes []byte
	var errs []error
	var err error

	leftBytes, err = os.ReadFile(left)
	if err != nil {
		return []error{err}
	}
	rightBytes, err = os.ReadFile(right)
	if err != nil {
		return []error{err}
	}

	commits := []*model.Commit{
		{
			Hash:       uuid.NewV4().String()[:6],
			Message:    fmt.Sprintf("New: %s, Original: %s", right, left),
			CommitDate: time.Now(),
			Data:       rightBytes,
		},
		{
			Hash:       uuid.NewV4().String()[:6],
			Message:    fmt.Sprintf("Original file: %s", left),
			CommitDate: time.Now(),
			Data:       leftBytes,
		},
	}

	commits, errs = git.BuildCommitChangelog(commits, updateChan, errorChan)
	if len(errs) > 0 {
		return errs
	}
	close(updateChan)
	//	close(errorChan)
	e := printSummaryDetails(commits)
	if e != nil {
		return []error{e}
	}
	return nil
}

func runGithubHistorySummary(username, repo, filePath string, latest bool, limit int,
	progressChan chan *model.ProgressUpdate, errorChan chan model.ProgressError) error {
	commitHistory, errs := git.ProcessGithubRepo(username, repo, filePath, progressChan, errorChan, false, limit)
	if errs != nil {
		return errs[0]
	}
	if latest {
		commitHistory = commitHistory[:1]
	}

	model.SendProgressUpdate("extraction",
		fmt.Sprintf("extracted %d commits from history", len(commitHistory)), true, progressChan)

	close(progressChan)

	return printSummaryDetails(commitHistory)
}

func runGitHistorySummary(gitPath, filePath string, latest bool,
	updateChan chan *model.ProgressUpdate, errorChan chan model.ProgressError) error {
	if gitPath == "" || filePath == "" {
		err := errors.New("please supply a path to a git repo via -r, and a path to a file via -f")
		model.SendProgressError("git", err.Error(), errorChan)
		return err
	}

	model.SendProgressUpdate("extraction",
		fmt.Sprintf("Extracting history for '%s' in repo '%s",
			filePath, gitPath), false, updateChan)

	// build commit history.
	commitHistory, errs := git.ExtractHistoryFromFile(gitPath, filePath, updateChan, errorChan)
	if errs != nil {
		model.SendProgressError("git", fmt.Sprintf("%d errors found extracting history", len(errs)), errorChan)
		return errs[0]
	}

	// populate history with changes and data
	git.PopulateHistoryWithChanges(commitHistory, 0, updateChan, errorChan)

	if latest {
		commitHistory = commitHistory[:1]
	}
	model.SendProgressUpdate("extraction",
		fmt.Sprintf("extracted %d commits from history", len(commitHistory)), true, updateChan)

	close(updateChan)

	return printSummaryDetails(commitHistory)
}

func printSummaryDetails(commitHistory []*model.Commit) error {
	tt := 0
	tb := 0
	pterm.Println()
	errorStyle := pterm.NewStyle(pterm.FgLightRed, pterm.Italic)
	for c := range commitHistory {
		tableData := [][]string{{"Document Element", "Total Changes", "Breaking Changes"}}

		if commitHistory[c].Changes != nil {
			if c == 0 {
				buildConsoleTree(commitHistory[c].Changes)
			}

			report := reports.CreateOverallReport(commitHistory[c].Changes)
			total := 0
			breaking := 0
			for l := range report.ChangeReport {
				total += report.ChangeReport[l].Total
				tt += total
				breaking += report.ChangeReport[l].Breaking
				tb += breaking
				tableData = append(tableData, []string{
					l,
					fmt.Sprint(report.ChangeReport[l].Total),
					fmt.Sprint(report.ChangeReport[l].Breaking),
				})
			}
			pterm.Printf("Date: %s | Commit: %s\n",
				commitHistory[c].CommitDate.Format("01/02/06"),
				commitHistory[c].Message)
			_ = pterm.DefaultTable.WithHasHeader().WithData(tableData).Render()
			if breaking == 0 {
				pterm.Info.Printf("Total Changes: %d\n", total)
			} else {
				errorStyle.Printf("❌ %d Breaking changes out of %d\n", breaking, total)
			}
			if c < len(commitHistory) {
				pterm.Println()
			}
		}
	}

	if tb > 0 {
		return errors.New("breaking changes discovered")
	} else {
		return nil
	}
}
