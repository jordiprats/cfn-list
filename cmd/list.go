package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	filterAll        bool
	filterComplete   bool
	filterDeleted    bool
	filterInProgress bool
	nameFilter       string
	descContains     string
	descNotContains  string
	namesOnly        bool
	resourceType     string
	resourceName     string
	properties       []string
)

func ListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list [name-filter]",
		Short: "List CloudFormation stacks",
		Long: `List CloudFormation stacks. By default shows active and in-progress stacks.

A name filter can be provided as a positional argument or via --name.

When resource filters (--type, --resource-name, --property) are specified, 
performs a deep search of stack templates and shows matching resources.

Examples:
  # List all stacks (table view)
  cfn list
  
  # Filter stacks by name
  cfn list my-stack
  
  # Search for stacks containing a specific resource type
  cfn list --type AWS::ServiceCatalog::CloudFormationProvisionedProduct
  
  # Search for stacks containing a resource with a specific logical ID
  cfn list --resource-name MyBucket
  
  # Combine filters
  cfn list my-stack --type AWS::S3::Bucket --property BucketName=foo`,
		Args: cobra.MaximumNArgs(1),
		Run:  runList,
	}

	cmd.Flags().BoolVarP(&filterAll, "all", "A", false, "Show all stacks (overrides other status filters)")
	cmd.Flags().BoolVarP(&filterComplete, "complete", "c", false, "Filter complete stacks (*_COMPLETE statuses)")
	cmd.Flags().BoolVarP(&filterDeleted, "deleted", "d", false, "Filter deleted stacks (DELETE_* statuses)")
	cmd.Flags().BoolVarP(&filterInProgress, "in-progress", "i", false, "Filter in-progress stacks (*_IN_PROGRESS statuses)")
	cmd.Flags().StringVarP(&nameFilter, "name", "n", "", "Filter stacks whose name contains this string (case-insensitive)")
	cmd.Flags().StringVar(&descContains, "desc", "", "Filter stacks whose description contains this string (case-insensitive)")
	cmd.Flags().StringVar(&descNotContains, "no-desc", "", "Exclude stacks whose description contains this string (case-insensitive)")
	cmd.Flags().BoolVarP(&namesOnly, "names-only", "1", false, "Print only stack names, one per line")
	cmd.Flags().StringVarP(&resourceType, "type", "t", "", "Search for resource type (e.g., AWS::S3::Bucket)")
	cmd.Flags().StringVarP(&resourceName, "resource-name", "R", "", "Search for resource logical ID")
	cmd.Flags().StringArrayVarP(&properties, "property", "p", []string{}, "Search for resource property (format: key=value or nested.key=value)")

	return cmd
}

func runList(cmd *cobra.Command, args []string) {
	// Positional arg is a shorthand for --name
	if len(args) > 0 && nameFilter == "" {
		nameFilter = args[0]
	} else if len(args) > 0 && nameFilter != "" {
		fatalf("Error: name filter specified both as argument and --name flag\n")
	}

	ctx := context.Background()
	client := mustClient(ctx)

	statusFilters := buildStatusFilters(filterAll, filterComplete, filterDeleted, filterInProgress)
	stacks, err := listStacks(ctx, client, statusFilters, nameFilter, descContains, descNotContains)
	if err != nil {
		fatalf("failed to list stacks: %v\n", err)
	}

	// Check if resource search is requested
	isResourceSearch := resourceType != "" || resourceName != "" || len(properties) > 0

	if isResourceSearch {
		runResourceSearch(ctx, client, stacks, namesOnly)
		return
	}

	if namesOnly {
		for _, s := range stacks {
			if s.StackName != nil {
				fmt.Println(*s.StackName)
			}
		}
		return
	}

	if len(stacks) == 0 {
		fmt.Fprintf(os.Stderr, "No stacks found\n")
		os.Exit(1)
	}

	printStacks(noHeaders, stacks)
}

func runResourceSearch(ctx context.Context, client *cloudformation.Client, stacks []types.StackSummary, namesOnly bool) {
	// Parse property filters
	propertyFilters := make(map[string]string)
	for _, prop := range properties {
		parts := strings.SplitN(prop, "=", 2)
		if len(parts) != 2 {
			fatalf("invalid property format %q, expected key=value\n", prop)
		}
		propertyFilters[parts[0]] = parts[1]
	}

	if len(stacks) == 0 {
		fmt.Fprintf(os.Stderr, "No stacks to search\n")
		os.Exit(1)
	}

	// Build search message (only show if not in names-only mode)
	if !namesOnly {
		searchMsg := fmt.Sprintf("Searching %d stacks", len(stacks))
		if resourceType != "" {
			searchMsg += fmt.Sprintf(" for resource type %q", resourceType)
		}
		if resourceName != "" {
			searchMsg += fmt.Sprintf(" for resource name %q", resourceName)
		}
		searchMsg += "..."
		fmt.Fprintf(os.Stderr, "%s\n", searchMsg)
	}

	var matchingStacks []stackMatch
	for _, stack := range stacks {
		if stack.StackName == nil {
			continue
		}

		matches, err := searchStackTemplate(ctx, client, *stack.StackName, resourceType, resourceName, propertyFilters)
		if err != nil {
			// Skip stacks we can't access
			continue
		}

		if len(matches) > 0 {
			matchingStacks = append(matchingStacks, stackMatch{
				stackName: *stack.StackName,
				resources: matches,
			})
		}
	}

	// Clear the "Searching..." line (only if we showed it)
	if !namesOnly {
		fmt.Fprintf(os.Stderr, "\033[1A\033[2K")
	}

	if len(matchingStacks) == 0 {
		if !namesOnly {
			fmt.Printf("No stacks found")
			if resourceType != "" {
				fmt.Printf(" containing resource type %q", resourceType)
			}
			if resourceName != "" {
				fmt.Printf(" containing resource name %q", resourceName)
			}
			if len(propertyFilters) > 0 {
				fmt.Printf(" with properties:")
				for key, value := range propertyFilters {
					fmt.Printf(" %s=%q", key, value)
				}
			}
			fmt.Println()
		}
		return
	}

	// Print results
	if namesOnly {
		// In names-only mode, just print stack names
		for _, match := range matchingStacks {
			fmt.Println(match.stackName)
		}
	} else {
		// Normal detailed output
		fmt.Printf("Found %d stack(s) with matching resources:\n\n", len(matchingStacks))
		for _, match := range matchingStacks {
			fmt.Printf("Stack: %s\n", match.stackName)
			for _, resource := range match.resources {
				fmt.Printf("  - %s (%s)\n", resource.logicalID, resource.resourceType)
				if len(resource.matchedProperties) > 0 {
					for key, value := range resource.matchedProperties {
						fmt.Printf("      %s: %v\n", key, value)
					}
				}
			}
			fmt.Println()
		}
	}
}

type stackMatch struct {
	stackName string
	resources []resourceMatch
}

type resourceMatch struct {
	logicalID         string
	resourceType      string
	matchedProperties map[string]interface{}
}

func searchStackTemplate(ctx context.Context, client *cloudformation.Client, stackName, resType, resName string, propertyFilters map[string]string) ([]resourceMatch, error) {
	// Get template
	output, err := client.GetTemplate(ctx, &cloudformation.GetTemplateInput{
		StackName:     &stackName,
		TemplateStage: types.TemplateStageOriginal,
	})
	if err != nil {
		return nil, err
	}

	body := getValue(output.TemplateBody)
	if body == "" {
		return nil, fmt.Errorf("empty template")
	}

	// Parse template (try JSON first, then YAML)
	var template map[string]interface{}
	if err := json.Unmarshal([]byte(body), &template); err != nil {
		// Try YAML
		if err := yaml.Unmarshal([]byte(body), &template); err != nil {
			return nil, fmt.Errorf("failed to parse template: %v", err)
		}
	}

	// Search for resources
	resources, ok := template["Resources"].(map[string]interface{})
	if !ok {
		return nil, nil
	}

	var matches []resourceMatch
	for logicalID, resourceData := range resources {
		resourceMap, ok := resourceData.(map[string]interface{})
		if !ok {
			continue
		}

		// Filter by resource type if specified
		currentType, ok := resourceMap["Type"].(string)
		if !ok {
			continue
		}
		if resType != "" && currentType != resType {
			continue
		}

		// Filter by resource name (logical ID) if specified
		if resName != "" && !strings.Contains(logicalID, resName) {
			continue
		}

		// Check if properties match
		if len(propertyFilters) > 0 {
			properties, ok := resourceMap["Properties"].(map[string]interface{})
			if !ok {
				continue
			}

			matched, matchedProps := checkProperties(properties, propertyFilters)
			if !matched {
				continue
			}

			matches = append(matches, resourceMatch{
				logicalID:         logicalID,
				resourceType:      currentType,
				matchedProperties: matchedProps,
			})
		} else {
			// No property filters, just match the type and/or name
			matches = append(matches, resourceMatch{
				logicalID:    logicalID,
				resourceType: currentType,
			})
		}
	}

	return matches, nil
}

func checkProperties(properties map[string]interface{}, filters map[string]string) (bool, map[string]interface{}) {
	matchedProps := make(map[string]interface{})

	for key, expectedValue := range filters {
		// Handle nested properties (e.g., "Versioning.Status")
		value := getNestedProperty(properties, key)
		if value == nil {
			return false, nil
		}

		// Convert value to string for comparison
		valueStr := fmt.Sprintf("%v", value)
		if valueStr != expectedValue {
			return false, nil
		}

		matchedProps[key] = value
	}

	return true, matchedProps
}

func getNestedProperty(properties map[string]interface{}, path string) interface{} {
	parts := strings.Split(path, ".")
	var current interface{} = properties

	for _, part := range parts {
		currentMap, ok := current.(map[string]interface{})
		if !ok {
			return nil
		}

		val, exists := currentMap[part]
		if !exists {
			return nil
		}

		current = val
	}

	return current
}
