# ssmx IAM Permissions

ssmx needs permissions on two principals: the **caller** (the developer running ssmx on their machine) and the **instance role** (the IAM role attached to the EC2 instance). These are independent — misconfiguring either one breaks SSM sessions.

> Cross-referenced against:
> - [Session Manager instance profile](https://docs.aws.amazon.com/systems-manager/latest/userguide/getting-started-create-iam-instance-profile.html)
> - [Session Manager caller permissions](https://docs.aws.amazon.com/systems-manager/latest/userguide/getting-started-restrict-access-quickstart.html)
> - [EC2 Instance Connect IAM reference](https://docs.aws.amazon.com/IAM/latest/UserGuide/list_amazonec2instanceconnect.html)

---

## Instance Role (EC2)

Every instance you want to reach via ssmx must have these permissions on its IAM role.

The simplest path is to attach the AWS-managed policy **`AmazonSSMManagedInstanceCore`** — it grants exactly the actions below.

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "SSMAgentCore",
      "Effect": "Allow",
      "Action": [
        "ssm:UpdateInstanceInformation",
        "ssmmessages:CreateControlChannel",
        "ssmmessages:CreateDataChannel",
        "ssmmessages:OpenControlChannel",
        "ssmmessages:OpenDataChannel"
      ],
      "Resource": "*"
    }
  ]
}
```

> **Note:** `ec2messages:*` actions are required for SSM Run Command but **not** for Session Manager sessions. ssmx does not use Run Command, so they are not listed here.

**Optional — session logging to S3 / CloudWatch:**

```json
{
  "Effect": "Allow",
  "Action": [
    "logs:CreateLogStream",
    "logs:PutLogEvents",
    "logs:DescribeLogGroups",
    "logs:DescribeLogStreams"
  ],
  "Resource": "*"
},
{
  "Effect": "Allow",
  "Action": "s3:PutObject",
  "Resource": "arn:aws:s3:::your-bucket/prefix/*"
},
{
  "Effect": "Allow",
  "Action": "s3:GetEncryptionConfiguration",
  "Resource": "*"
},
{
  "Effect": "Allow",
  "Action": "kms:Decrypt",
  "Resource": "arn:aws:kms:region:account:key/key-id"
}
```

---

## Caller Permissions (Developer)

Three tiers — start with the minimum and add tiers as you use more features.

### Tier 1 — Core (connect, exec, list, port forwarding)

Covers `ssmx <target>`, `ssmx <target> -- cmd`, `ssmx -l`, and `ssmx <target> -L`.

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "SsmxDescribe",
      "Effect": "Allow",
      "Action": [
        "ec2:DescribeInstances",
        "ssm:DescribeInstanceInformation",
        "ssm:DescribeSessions",
        "ssm:GetConnectionStatus"
      ],
      "Resource": "*"
    },
    {
      "Sid": "SsmxSession",
      "Effect": "Allow",
      "Action": [
        "ssm:StartSession"
      ],
      "Resource": [
        "arn:aws:ec2:*:*:instance/*",
        "arn:aws:ssm:*:*:document/AWS-StartInteractiveCommand",
        "arn:aws:ssm:*:*:document/AWS-StartPortForwardingSession",
        "arn:aws:ssm:*:*:document/AWS-StartPortForwardingSessionToRemoteHost"
      ]
    },
    {
      "Sid": "SsmxSessionSelf",
      "Effect": "Allow",
      "Action": [
        "ssm:TerminateSession",
        "ssm:ResumeSession",
        "ssmmessages:OpenDataChannel"
      ],
      "Resource": "arn:aws:ssm:*:*:session/${aws:userid}-*"
    }
  ]
}
```

> **`ssmmessages:OpenDataChannel`** on the caller side: the AWS docs show this scoped to `session/${aws:userid}-*` in their example policies. It is separate from the same action on the instance role (which is scoped to `*`).

> **`ssm:ResumeSession`** handles reconnecting to an existing session after a dropped connection — not just starting fresh. Required for robust use.

### Tier 2 — SSH Proxy (adds `ssh`, `scp`, `rsync`, VS Code Remote)

Add to Tier 1 when using `ssmx --configure` → SSH config generation and `ssmx --proxy`.

```json
{
  "Sid": "SsmxSSHProxy",
  "Effect": "Allow",
  "Action": [
    "ssm:StartSession"
  ],
  "Resource": [
    "arn:aws:ec2:*:*:instance/*",
    "arn:aws:ssm:*:*:document/AWS-StartSSHSession"
  ]
},
{
  "Sid": "SsmxInstanceConnect",
  "Effect": "Allow",
  "Action": "ec2-instance-connect:SendSSHPublicKey",
  "Resource": "arn:aws:ec2:*:*:instance/*"
}
```

> **`ec2:osuser` condition key:** EC2 Instance Connect supports restricting which OS users can receive a pushed key. For example, to allow only `ec2-user` and `ubuntu`:
> ```json
> "Condition": {
>   "StringEquals": {
>     "ec2:osuser": ["ec2-user", "ubuntu"]
>   }
> }
> ```
> This is optional but useful in environments with shared bastion-style access.

> **Instance requirement:** Instances must have the `ec2-instance-connect` package installed. Pre-installed on Amazon Linux 2, Amazon Linux 2023, and Ubuntu 20.04+. Install manually on other distros.

### Tier 3 — Health Check (adds `ssmx <target> --health`)

Add to Tier 1 (and Tier 2 if using SSH proxy) when using `ssmx --health`.

```json
{
  "Sid": "SsmxHealth",
  "Effect": "Allow",
  "Action": [
    "sts:GetCallerIdentity",
    "iam:GetInstanceProfile",
    "iam:SimulatePrincipalPolicy",
    "ec2:DescribeVpcEndpoints"
  ],
  "Resource": "*"
}
```

> **`iam:SimulatePrincipalPolicy`** simulates IAM policy evaluation without making real API calls — this is how `--health` checks both caller and instance role permissions. Some organisations restrict it. If denied, `ssmx --health` degrades gracefully: it skips the IAM simulation checks and shows the instance profile ARN for manual inspection instead.

---

## Full Policy (all tiers combined)

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "SsmxDescribe",
      "Effect": "Allow",
      "Action": [
        "ec2:DescribeInstances",
        "ec2:DescribeVpcEndpoints",
        "ssm:DescribeInstanceInformation",
        "ssm:DescribeSessions",
        "ssm:GetConnectionStatus",
        "sts:GetCallerIdentity",
        "iam:GetInstanceProfile",
        "iam:SimulatePrincipalPolicy"
      ],
      "Resource": "*"
    },
    {
      "Sid": "SsmxSession",
      "Effect": "Allow",
      "Action": "ssm:StartSession",
      "Resource": [
        "arn:aws:ec2:*:*:instance/*",
        "arn:aws:ssm:*:*:document/AWS-StartInteractiveCommand",
        "arn:aws:ssm:*:*:document/AWS-StartPortForwardingSession",
        "arn:aws:ssm:*:*:document/AWS-StartPortForwardingSessionToRemoteHost",
        "arn:aws:ssm:*:*:document/AWS-StartSSHSession"
      ]
    },
    {
      "Sid": "SsmxSessionSelf",
      "Effect": "Allow",
      "Action": [
        "ssm:TerminateSession",
        "ssm:ResumeSession",
        "ssmmessages:OpenDataChannel"
      ],
      "Resource": "arn:aws:ssm:*:*:session/${aws:userid}-*"
    },
    {
      "Sid": "SsmxInstanceConnect",
      "Effect": "Allow",
      "Action": "ec2-instance-connect:SendSSHPublicKey",
      "Resource": "arn:aws:ec2:*:*:instance/*"
    }
  ]
}
```

---

## Restricting by tag

Scope `ssm:StartSession` with `ssm:resourceTag` (not `aws:ResourceTag` — the Session Manager docs use the SSM-specific condition key):

```json
{
  "Sid": "SsmxSessionTagged",
  "Effect": "Allow",
  "Action": "ssm:StartSession",
  "Resource": "arn:aws:ec2:*:*:instance/*",
  "Condition": {
    "StringLike": {
      "ssm:resourceTag/ssmx-access": ["true"]
    }
  }
}
```

Similarly scope `ec2-instance-connect:SendSSHPublicKey` using `ec2:ResourceTag`:

```json
{
  "Sid": "SsmxInstanceConnectTagged",
  "Effect": "Allow",
  "Action": "ec2-instance-connect:SendSSHPublicKey",
  "Resource": "arn:aws:ec2:*:*:instance/*",
  "Condition": {
    "StringLike": {
      "ec2:ResourceTag/ssmx-access": ["true"]
    }
  }
}
```

Tag instances with `ssmx-access=true` to permit access. Untagged instances are unreachable even if SSM is healthy.

---

## What was changed from the initial draft (cross-reference notes)

| Item | Initial | Corrected |
|---|---|---|
| `ssm:GetConnectionStatus` | missing | added to Tier 1 |
| `ssm:ResumeSession` | missing | added to Tier 1 |
| `ssm:DescribeSessions` | missing | added to Tier 1 |
| `ssmmessages:OpenDataChannel` (caller) | missing | added to Tier 1, scoped to `session/${aws:userid}-*` |
| `ec2messages:*` on instance role | included (5 actions) | removed — only needed for Run Command, not Session Manager |
| `ec2:osuser` condition key | not mentioned | added to Tier 2 SSH proxy section |
| Tag condition key | `aws:ResourceTag` | corrected to `ssm:resourceTag` for SSM, `ec2:ResourceTag` for EC2 Instance Connect |

---

## Summary table

| Feature | Caller permissions needed |
|---|---|
| `ssmx -l` | `ec2:DescribeInstances`, `ssm:DescribeInstanceInformation` |
| `ssmx <target>` | + `ssm:StartSession`, `ssm:TerminateSession`, `ssm:ResumeSession`, `ssm:GetConnectionStatus`, `ssmmessages:OpenDataChannel` |
| `ssmx <target> -- cmd` | same as connect |
| `ssmx <target> -L` | + `ssm:StartSession` on forwarding documents |
| `ssmx --proxy` (SSH) | + `ec2-instance-connect:SendSSHPublicKey`, `ssm:StartSession` on `AWS-StartSSHSession` |
| `ssmx --health` | + `sts:GetCallerIdentity`, `iam:GetInstanceProfile`, `iam:SimulatePrincipalPolicy`, `ec2:DescribeSubnets`, `ec2:DescribeVpcEndpoints` |
