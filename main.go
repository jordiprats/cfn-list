package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
)

type StatusFilter int

const (
	FilterActive StatusFilter = iota
	FilterComplete
	FilterDeleted
	FilterInProgress
)

func main() {
	var (
		filterActive     bool
		filterComplete   bool
		filterDeleted    bool
		filterInProgress bool
		nameFilter       string
		region           string
	)

	flag.BoolVar(&filterActive, "active", false, "Filter active stacks (excludes deleted and failed)")
	flag.BoolVar(&filterComplete, "complete", false, "Filter complete stacks")
	flag.BoolVar(&filterDeleted, "deleted", false, "Filter deleted stacks")
	flag.BoolVar(&filterInProgress, "in-progress", false, "Filter in-progress stacks")
	flag.StringVar(&nameFilter, "name", "", "Filter stacks containing this string in name")
	flag.StringVar(&region, "region", "", "AWS region (uses default if not specified)")
	flag.Parse()

	ctx := context.Background()

	// Load AWS config
	cfg, err := config.LoadDefaultConfig(ctx, func(opts *config.LoadOptions) error {
		if region != "" {
			opts.Region = region
		}
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	client := cloudformation.NewFromConfig(cfg)

	// Get all stacks
	stacks, err := listAllStacks(ctx, client)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to list stacks: %v\n", err)
		os.Exit(1)
	}

	// Apply filters
	filtered := filterStacks(stacks, filterActive, filterComplete, filterDeleted, filterInProgress, nameFilter)

	// Display results
	if len(filtered) == 0 {
		fmt.Println("No stacks found matching criteria")
		return
	}

	fmt.Printf("%-50s %-30s %-20s\n", "STACK NAME", "STATUS", "CREATION TIME")
	fmt.Println(strings.Repeat("-", 100))

	for _, stack := range filtered {
		creationTime := ""
		if stack.CreationTime != nil {
			creationTime = stack.CreationTime.Format("2006-01-02 15:04:05")
		}
		fmt.Printf("%-50s %-30s %-20s\n",
			truncate(*stack.StackName, 50),
			stack.StackStatus,
			creationTime)
	}

	fmt.Printf("\nTotal: %d stacks\n", len(filtered))
}

func listAllStacks(ctx context.Context, client *cloudformation.Client) ([]types.StackSummary, error) {
	var allStacks []types.StackSummary
	paginator := cloudformation.NewListStacksPaginator(client, &cloudformation.ListStacksInput{})

	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		allStacks = append(allStacks, output.StackSummaries...)
	}

	return allStacks, nil
}

func filterStacks(stacks []types.StackSummary, active, complete, deleted, inProgress bool, nameFilter string) []types.StackSummary {
	var filtered []types.StackSummary

	// If no status filters specified, show "active" stacks (not deleted)
	noStatusFilter := !active && !complete && !deleted && !inProgress

	for _, stack := range stacks {
		// Apply name filter
		if nameFilter != "" && !strings.Contains(strings.ToLower(*stack.StackName), strings.ToLower(nameFilter)) {
			continue
		}

		status := string(stack.StackStatus)

		// Apply status filters
		if noStatusFilter {
			// Default: show all non-deleted stacks
			if !strings.HasPrefix(status, "DELETE_COMPLETE") {
				filtered = append(filtered, stack)
			}
			continue
		}

		matched := false

		if active && isActiveStatus(status) {
			matched = true
		}
		if complete && isCompleteStatus(status) {
			matched = true
		}
		if deleted && isDeletedStatus(status) {
			matched = true
		}
		if inProgress && isInProgressStatus(status) {
			matched = true
		}

		if matched {
			filtered = append(filtered, stack)
		}
	}

	return filtered
}

func isActiveStatus(status string) bool {
	// Active = successfully completed or stable state (not deleted, not failed)
	completeStates := []string{
		"CREATE_COMPLETE",
		"UPDATE_COMPLETE",
		"ROLLBACK_COMPLETE",
		"UPDATE_ROLLBACK_COMPLETE",
		"IMPORT_COMPLETE",
		"IMPORT_ROLLBACK_COMPLETE",
	}

	for _, state := range completeStates {
		if status == state {
			return true
		}
	}
	return false
}

func isCompleteStatus(status string) bool {
	return strings.HasSuffix(status, "_COMPLETE")
}

func isDeletedStatus(status string) bool {
	return strings.HasPrefix(status, "DELETE_")
}

func isInProgressStatus(status string) bool {
	return strings.HasSuffix(status, "_IN_PROGRESS")
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}