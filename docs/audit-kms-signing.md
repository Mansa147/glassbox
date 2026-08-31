# AWS KMS Audit Signing

Glassbox supports AWS Key Management Service (KMS) as an optional signing provider for audit logs, in addition to the built-in software and PKCS#11 providers.

## Overview

AWS KMS provides hardware-level security for audit log signatures. Private keys never leave AWS KMS, and signing operations are performed within the AWS cloud.

## Requirements

- AWS KMS key with Ed25519 key spec
- AWS credentials with `kms:Sign` and `kms:GetPublicKey` permissions
- AWS SDK for Go v2 (automatically included)

## AWS IAM Policy

Your IAM user/role needs the following permissions:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "AllowGlassboxSigning",
      "Effect": "Allow",
      "Action": [
        "kms:Sign",
        "kms:GetPublicKey",
        "kms:DescribeKey"
      ],
      "Resource": "arn:aws:kms:REGION:ACCOUNT_ID:key/KEY_ID"
    }
  ]
}
```

## Creating an Ed25519 KMS Key

```bash
aws kms create-key \
  --key-usage SIGN_VERIFY \
  --key-spec ED25519 \
  --description "Glassbox audit signing key"
```

Save the key ID or alias for use with Glassbox.

## Usage

### Via Environment Variables

```bash
export GLASSBOX_SIGNING_PROVIDER=aws-kms
export GLASSBOX_AWS_KMS_KEY_ID=alias/GlassboxAuditKey
export GLASSBOX_AWS_KMS_REGION=us-east-1

# Optional: AWS credentials (or use default credential chain)
export AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE
export AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY

glassbox audit:sign --payload-file data.json
```

### Via Config File

```toml
[audit]
signing_provider = "aws-kms"

[audit.kms]
key_id = "alias/GlassboxAuditKey"
region = "us-east-1"

# Optional: explicit credentials
# aws_access_key_id = "..."
# aws_secret_access_key = "..."
# aws_profile = "default"
```

### Via CLI Flags

```bash
glassbox audit:sign \
  --payload-file data.json \
  --signing-provider aws-kms \
  --audit-log-kms-key-id alias/GlassboxAuditKey \
  --audit-log-kms-region us-east-1
```

## Supported Key Identifiers

| Format | Example |
|--------|---------|
| Key alias | `alias/GlassboxAuditKey` |
| Key ARN | `arn:aws:kms:us-east-1:123456789012:key/12345678-1234-1234-1234-123456789012` |
| Key ID | `12345678-1234-1234-1234-123456789012` |

## Credential Precedence

AWS credentials are resolved in the following order:

1. Explicit credentials in config (`aws_access_key_id` / `aws_secret_access_key`)
2. AWS profile from config (`aws_profile`)
3. Default credential chain (environment → config file → EC2 role → ECS task)

## Error Handling

The KMS provider includes proper error handling for:

- Invalid key IDs
- Missing permissions
- Network/connectivity issues
- Key state issues (pending deletion, disabled, etc.)

## Verification

To verify KMS signing is working:

```bash
# Sign an audit log
glassbox audit:sign --payload-file data.json --audit-log signed-audit.json

# Verify the signature
glassbox audit:verify --audit-log signed-audit.json
```

---

## Architecture Decision Records

The following ADRs govern the design decisions behind KMS signing:

- [ADR-006: Provider Isolation](adr/006-provider-isolation.md) — explains why the KMS provider never exposes the private key to the host process, the minimum IAM permissions required, and why there is no fallback chain.
- [ADR-003: Trust Boundaries and Component Trust Levels](adr/003-trust-boundaries.md) — classifies the KMS provider as Tier 3 (trusted-by-configuration) and documents the controls at the signing provider boundary.
- [ADR-007: Offline Guarantees](adr/007-offline-guarantees.md) — documents that the KMS provider requires network access for every signing call and is not compatible with air-gapped workflows.
- [ADR-004: Data Classification and Cross-Boundary Data Flows](adr/004-data-classification.md) — confirms that only the 32-byte SHA-256 digest (not the canonical payload) crosses the boundary to the KMS API.
