---
layout: default
parent: CoreOS Assembler Command Line Reference
nav_order: 1
---

# cosa buildextend-aws

Using `cosa buildextend-aws` looks for AWS credentials in the standard places;
Common methods are `AWS_ACCESS_KEY_ID`
and `AWS_SECRET_ACCESS_KEY` environment variables, and using `AWS_PROFILE`.
For more information, consult the documentation for the `aws` CLI tool, in particular
`aws configure help`.

Uploading AMIs [requires IAM permissions](https://docs.aws.amazon.com/vm-import/latest/userguide/vmie_prereqs.html#iam-permissions-image).

Follow the [instructions to create the vmimport role](https://docs.aws.amazon.com/vm-import/latest/userguide/vmimport-image-import.html) and attach it to the bot account + bucket:

```json
    {
        "Effect": "Allow",
        "Action": [
            "ec2:CopyImage",
            "ec2:CopySnapshot",
            "ec2:CreateTags",
            "ec2:Describe*",
            "ec2:ImportSnapshot",
            "ec2:ModifyImageAttribute",
            "ec2:ModifySnapshotAttribute",
            "ec2:RegisterImage"
        ],
        "Resource": "*"
    }
```

This is currently a wrapper around the `ore` subcommand of mantle; there is an
`ore aws initialize` subcommand which may be useful.
