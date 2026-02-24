## cfn search

Search for stacks containing a specific resource type

### Synopsis

Search for stacks containing resources of a specific type. By default searches active and in-progress stacks.

Optionally filter by resource properties using --property flags.

Examples:
  cfn search AWS::ServiceCatalog::CloudFormationProvisionedProduct
  cfn search AWS::ServiceCatalog::CloudFormationProvisionedProduct --property ProductName=IAMRole
  cfn search AWS::S3::Bucket --property BucketName=my-bucket --property Versioning.Status=Enabled

```
cfn search <resource-type> [flags]
```

### Options

```
  -A, --all                    Show all stacks (overrides other status filters)
  -c, --complete               Filter complete stacks (*_COMPLETE statuses)
  -d, --deleted                Filter deleted stacks (DELETE_* statuses)
  -h, --help                   help for search
  -i, --in-progress            Filter in-progress stacks (*_IN_PROGRESS statuses)
  -p, --property stringArray   Filter by property (format: key=value or nested.key=value)
```

### Options inherited from parent commands

```
      --no-headers      Don't print headers
  -r, --region string   AWS region (uses default if not specified)
```

### SEE ALSO

* [cfn](cfn.md)	 - AWS CloudFormation CLI tool

