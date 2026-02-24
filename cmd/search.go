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
	searchProperties       []string
	searchFilterAll        bool
	searchFilterComplete   bool
	searchFilterDeleted    bool
	searchFilterInProgress bool
)

func SearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search <resource-type>",
		Short: "Search for stacks containing a specific resource type",
		Long: `Search for stacks containing resources of a specific type. By default searches active and in-progress stacks.

Optionally filter by resource properties using --property flags.

Examples:
  cfn search AWS::ServiceCatalog::CloudFormationProvisionedProduct
  cfn search AWS::ServiceCatalog::CloudFormationProvisionedProduct --property ProductName=IAMRole
  cfn search AWS::S3::Bucket --property BucketName=my-bucket --property Versioning.Status=Enabled`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			runSearch(args[0])
		},
	}

	cmd.Flags().StringArrayVarP(&searchProperties, "property", "p", []string{}, "Filter by property (format: key=value or nested.key=value)")
	cmd.Flags().BoolVarP(&searchFilterAll, "all", "A", false, "Show all stacks (overrides other status filters)")
	cmd.Flags().BoolVarP(&searchFilterComplete, "complete", "c", false, "Filter complete stacks (*_COMPLETE statuses)")
	cmd.Flags().BoolVarP(&searchFilterDeleted, "deleted", "d", false, "Filter deleted stacks (DELETE_* statuses)")
	cmd.Flags().BoolVarP(&searchFilterInProgress, "in-progress", "i", false, "Filter in-progress stacks (*_IN_PROGRESS statuses)")

	return cmd
}

func runSearch(resourceType string) {
	ctx := context.Background()
	client := mustClient(ctx)

	// Parse property filters
	propertyFilters := make(map[string]string)
	for _, prop := range searchProperties {
		parts := strings.SplitN(prop, "=", 2)
		if len(parts) != 2 {
			fatalf("invalid property format %q, expected key=value\n", prop)
		}
		propertyFilters[parts[0]] = parts[1]
	}

	// List stacks with status filters
	statusFilters := buildStatusFilters(searchFilterAll, searchFilterComplete, searchFilterDeleted, searchFilterInProgress)
	stacks, err := listStacks(ctx, client, statusFilters, "", "", "")
	if err != nil {
		fatalf("failed to list stacks: %v\n", err)
	}

	if len(stacks) == 0 {
		fmt.Fprintf(os.Stderr, "No stacks to search\n")
		os.Exit(1)
	}

	fmt.Printf("Searching %d stacks for resource type %q...\n\n", len(stacks), resourceType)

	var matchingStacks []stackMatch
	for _, stack := range stacks {
		if stack.StackName == nil {
			continue
		}

		matches, err := searchStackTemplate(ctx, client, *stack.StackName, resourceType, propertyFilters)
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

	if len(matchingStacks) == 0 {
		fmt.Printf("No stacks found containing resource type %q", resourceType)
		if len(propertyFilters) > 0 {
			fmt.Printf(" with specified properties")
		}
		fmt.Println()
		return
	}

	// Print results
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

type stackMatch struct {
	stackName string
	resources []resourceMatch
}

type resourceMatch struct {
	logicalID         string
	resourceType      string
	matchedProperties map[string]interface{}
}

func searchStackTemplate(ctx context.Context, client *cloudformation.Client, stackName, resourceType string, propertyFilters map[string]string) ([]resourceMatch, error) {
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

		resType, ok := resourceMap["Type"].(string)
		if !ok || resType != resourceType {
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
				resourceType:      resType,
				matchedProperties: matchedProps,
			})
		} else {
			// No property filters, just match the type
			matches = append(matches, resourceMatch{
				logicalID:    logicalID,
				resourceType: resType,
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
