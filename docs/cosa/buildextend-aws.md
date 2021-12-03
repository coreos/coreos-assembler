---
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

Follow the [instructions to create the vmimport role](https://docs.aws.amazon.com/vm-import/latest/userguide/vmimport-image-import.html) and attach it to the bot account + bucket.

The full list of permisions required for your IAM policy should look similar to this:

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "s3:GetBucketLocation",
                "s3:GetObject",
                "s3:ListBucket",
                "s3:DeleteObject"
            ],
            "Resource": [
                "arn:aws:s3:::<name-of-s3-bucket>",
                "arn:aws:s3:::<name-of-s3-bucket>/*"
            ]
        },
        {
            "Effect": "Allow",
            "Action": [
                "ec2:ModifySnapshotAttribute",
                "ec2:CopySnapshot",
                "ec2:RegisterImage",
                "ec2:Describe*",
                "ec2:CancelConversionTask",
                "ec2:CancelExportTask",
                "ec2:CreateImage",
                "ec2:CreateInstanceExportTask",
                "ec2:CreateTags",
                "ec2:DescribeConversionTasks",
                "ec2:DescribeExportTasks",
                "ec2:DescribeExportImageTasks",
                "ec2:DescribeImages",
                "ec2:DescribeInstanceStatus",
                "ec2:DescribeInstances",
                "ec2:DescribeSnapshots",
                "ec2:DescribeTags",
                "ec2:ExportImage",
                "ec2:ImportInstance",
                "ec2:ImportVolume",
                "ec2:StartInstances",
                "ec2:StopInstances",
                "ec2:TerminateInstances",
                "ec2:ImportImage",
                "ec2:ImportSnapshot",
                "ec2:DescribeImportImageTasks",
                "ec2:DescribeImportSnapshotTasks",
                "ec2:CancelImportTask"
            ],
            "Resource": "*"
        },
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
    ]
}
```

This is currently a wrapper around the `ore` subcommand of mantle; there is an
`ore aws initialize` subcommand which may be useful.
