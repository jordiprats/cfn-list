## cfn list

List CloudFormation stacks

### Synopsis

List CloudFormation stacks. By default shows active and in-progress stacks.

A name filter can be provided as a positional argument or via --name.

```
cfn list [name-filter] [flags]
```

### Options

```
  -A, --all              Show all stacks (overrides other status filters)
  -c, --complete         Filter complete stacks (*_COMPLETE statuses)
  -d, --deleted          Filter deleted stacks (DELETE_* statuses)
      --desc string      Filter stacks whose description contains this string (case-insensitive)
  -h, --help             help for list
  -i, --in-progress      Filter in-progress stacks (*_IN_PROGRESS statuses)
  -n, --name string      Filter stacks whose name contains this string (case-insensitive)
  -1, --names-only       Print only stack names, one per line
      --no-desc string   Exclude stacks whose description contains this string (case-insensitive)
```

### Options inherited from parent commands

```
      --no-headers      Don't print headers
  -r, --region string   AWS region (uses default if not specified)
```

### SEE ALSO

* [cfn](cfn.md)	 - AWS CloudFormation CLI tool

