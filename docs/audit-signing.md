# Audit Signing

Glassbox can sign audit logs with either a software Ed25519 key or a PKCS#11
hardware security module (HSM). This document covers setup, environment
variables, platform-specific module paths, and troubleshooting.

## Quick start

```bash
# Software signing (Ed25519 PEM key)
glassbox audit:sign \
  --payload '{"input":{},"state":{},"events":[],"timestamp":"2026-01-01T00:00:00.000Z"}' \
  --software-private-key "$(cat ./ed25519-private-key.pem)"

# PKCS#11 signing
export GLASSBOX_PKCS11_MODULE=/usr/lib/softhsm/libsofthsm2.so
export GLASSBOX_PKCS11_PIN=1234
export GLASSBOX_PKCS11_KEY_LABEL=glassbox-audit-key
glassbox audit:sign --signing-provider pkcs11 --payload-file payload.json

# ...or pass everything as flags (flags override the environment)
glassbox audit:sign --signing-provider pkcs11 --payload-file payload.json \
  --pkcs11-module /usr/lib/softhsm/libsofthsm2.so --pkcs11-pin 1234 \
  --pkcs11-key-label glassbox-audit-key

# Validate PKCS#11 configuration without signing anything (preflight)
glassbox audit:sign --signing-provider pkcs11 --validate-only \
  --pkcs11-module /usr/lib/softhsm/libsofthsm2.so --pkcs11-pin 1234
```

---

## Software signing

Provide a PKCS#8 PEM-encoded Ed25519 private key via one of:

| Method | How |
|--------|-----|
| CLI flag | `--software-private-key "$(cat key.pem)"` |
| CLI flag (file path) | `--software-private-key ./key.pem` |
| Environment variable | `GLASSBOX_AUDIT_PRIVATE_KEY_PEM` |

Generate a key with OpenSSL:

```bash
openssl genpkey -algorithm ed25519 -out ed25519-private-key.pem
openssl pkey -in ed25519-private-key.pem -pubout -out ed25519-public-key.pem
```

### Software provider input validation

The key is validated at configuration time (before signing begins) so format
errors surface immediately with actionable remediation:

**Wrong PEM format — OpenSSH:**
```
the key is in OpenSSH format; Glassbox requires PKCS#8 PEM format
  Fix: convert with: openssl pkey -in key.pem -out key_pkcs8.pem
  Or generate a new key: openssl genpkey -algorithm ed25519 -out key.pem
```

**Wrong PEM format — SEC1 (EC PRIVATE KEY):**
```
the key is in SEC1 (EC PRIVATE KEY) format; Glassbox requires PKCS#8 PEM format
  Fix: convert with: openssl pkcs8 -topk8 -nocrypt -in key.pem -out key_pkcs8.pem
```

**RSA key:**
```
the key is an RSA key; Glassbox requires an Ed25519 PKCS#8 PEM private key
  Fix: generate a new Ed25519 key: openssl genpkey -algorithm ed25519 -out key.pem
```

**Garbled PEM:**
```
invalid Ed25519 private key: ...
  Expected: a PKCS#8 PEM file starting with '-----BEGIN PRIVATE KEY-----'
  Generate: openssl genpkey -algorithm ed25519 -out key.pem
```

**Hex key — wrong length:**
```
GLASSBOX_SOFTWARE_PRIVATE_KEY_HEX has wrong key length: got 10 bytes, expected 32 (seed) or 64 (full key)
  A 32-byte seed = 64 hex characters; a 64-byte full key = 128 hex characters
```

**Hex key — non-hex characters:**
```
GLASSBOX_SOFTWARE_PRIVATE_KEY_HEX is not valid hexadecimal: ...
  Expected: a 64-character hex string (32-byte seed) or 128-character hex string (64-byte full key)
  Fix: re-export the key or set GLASSBOX_AUDIT_PRIVATE_KEY_PEM with a PEM file instead
```

---

## PKCS#11 HSM signing

### Required environment variables

| Variable | Description |
|----------|-------------|
| `GLASSBOX_PKCS11_MODULE` | Absolute path to the PKCS#11 shared library |
| `GLASSBOX_PKCS11_PIN` | User PIN for the token |
| `GLASSBOX_PKCS11_KEY_LABEL` | `CKA_LABEL` of the Ed25519 private key |

At least one of `GLASSBOX_PKCS11_KEY_LABEL` or `GLASSBOX_PKCS11_KEY_ID` is required.

### Optional environment variables

| Variable | Description |
|----------|-------------|
| `GLASSBOX_PKCS11_KEY_ID` | Hex-encoded `CKA_ID` of the key (alternative to label) |
| `GLASSBOX_PKCS11_TOKEN_LABEL` | Select token by label instead of slot index |
| `GLASSBOX_PKCS11_SLOT` | Slot index (default `0`; must be a non-negative integer) |

### CLI flags and precedence

Every environment variable has a matching flag; **the flag takes precedence** when both are set:

| Flag | Environment variable |
|------|----------------------|
| `--pkcs11-module` | `GLASSBOX_PKCS11_MODULE` |
| `--pkcs11-pin` | `GLASSBOX_PKCS11_PIN` |
| `--pkcs11-token-label` | `GLASSBOX_PKCS11_TOKEN_LABEL` |
| `--pkcs11-key-label` | `GLASSBOX_PKCS11_KEY_LABEL` |
| `--pkcs11-key-id` | `GLASSBOX_PKCS11_KEY_ID` |

### Input validation

When the pkcs11 provider is selected, the command validates the configuration
**before loading any module**, so misconfiguration fails fast with an explicit
message instead of a low-level error mid-signing:

- `--pkcs11-module` (or `GLASSBOX_PKCS11_MODULE`) and `--pkcs11-pin` (or
  `GLASSBOX_PKCS11_PIN`) are required. A missing value is reported as, e.g.:

  ```
  pkcs11 signing requires --pkcs11-module (or GLASSBOX_PKCS11_MODULE) and --pkcs11-pin (or GLASSBOX_PKCS11_PIN)
    Provide the missing value(s), or run 'glassbox audit:sign --validate-only --signing-provider pkcs11' for a full PKCS#11 preflight report.
  ```

- **Module file existence** is checked before any signing work begins. A missing
  or inaccessible file is reported with a `Fix:` hint:

  ```
  --pkcs11-module: module file not found: "/usr/lib/nonexistent.so"
    Fix: verify the path is correct for your OS and architecture
    Tip: run 'glassbox audit:sign --validate-only --signing-provider pkcs11' for a full preflight report
  ```

  If the path points to a directory instead of a `.so`/`.dylib`/`.dll` file:

  ```
  --pkcs11-module: "/usr/lib/softhsm" is a directory, not a shared library
    Fix: provide the full path to the .so/.dylib/.dll file, not its parent directory
  ```

- **Key selector required** — at least one of `--pkcs11-key-label` (or
  `GLASSBOX_PKCS11_KEY_LABEL`) or `--pkcs11-key-id` (or `GLASSBOX_PKCS11_KEY_ID`)
  must be provided. Without a selector the signing attempt would fail at the
  key-lookup step; this check surfaces the issue before any module is loaded:

  ```
  no PKCS#11 key selector provided — set --pkcs11-key-label (or GLASSBOX_PKCS11_KEY_LABEL) or --pkcs11-key-id (or GLASSBOX_PKCS11_KEY_ID)
    Tip: run 'pkcs11-tool --list-objects --type privkey' to list available key labels
  ```

- `--pkcs11-key-id`, when provided, must be valid hex (`CKA_ID`):

  ```
  --pkcs11-key-id must be a hex-encoded CKA_ID (e.g. a1b2c3): encoding/hex: invalid byte ...
    Fix: provide a valid hex string, e.g. 'a1b2c3' (use pkcs11-tool --list-objects to find the CKA_ID)
  ```

- `GLASSBOX_PKCS11_SLOT`, when provided, must be a non-negative integer. Invalid
  values are rejected before preflight/signing starts so they do not silently
  fall back to slot `0`:

  ```
  GLASSBOX_PKCS11_SLOT must be a non-negative integer, got "not-a-number"
    Fix: set GLASSBOX_PKCS11_SLOT to a numeric slot index such as 0 or 1, or unset it to use the default slot
    Tip: run 'glassbox audit:sign --validate-only --signing-provider pkcs11' to inspect available slot diagnostics
  ```

### Preflight (`--validate-only`)

`--validate-only` runs the full PKCS#11 preflight — module path, module load,
slot enumeration, token info, session open, PIN auth, key lookup, and a
test-sign — and prints a `[PASS]`/`[FAIL]` report **without signing**. It works
with the provider selected by `--signing-provider pkcs11` (or the deprecated
`--hsm-provider pkcs11`, or the environment), and honors the `--pkcs11-*` flags
in addition to the environment variables. Each failed step includes a
remediation hint; the command exits non-zero if any check fails.

---

## Platform-specific module paths

### Linux (Debian / Ubuntu)

```bash
# SoftHSM2 (software HSM for testing)
apt install softhsm2
export GLASSBOX_PKCS11_MODULE=/usr/lib/softhsm/libsofthsm2.so
# or on x86_64:
export GLASSBOX_PKCS11_MODULE=/usr/lib/x86_64-linux-gnu/softhsm/libsofthsm2.so

# OpenSC (smart cards, YubiKey via CCID)
apt install opensc
export GLASSBOX_PKCS11_MODULE=/usr/lib/x86_64-linux-gnu/opensc-pkcs11.so

# YubiKey YKCS11
apt install ykcs11
export GLASSBOX_PKCS11_MODULE=/usr/lib/x86_64-linux-gnu/libykcs11.so
```

### Linux (RHEL / Fedora / CentOS)

```bash
# SoftHSM2
dnf install softhsm
export GLASSBOX_PKCS11_MODULE=/usr/lib64/softhsm/libsofthsm2.so

# OpenSC
dnf install opensc
export GLASSBOX_PKCS11_MODULE=/usr/lib64/opensc-pkcs11.so

# YubiKey YKCS11
dnf install ykcs11
export GLASSBOX_PKCS11_MODULE=/usr/lib64/libykcs11.so
```

### macOS

```bash
# SoftHSM2
brew install softhsm
export GLASSBOX_PKCS11_MODULE=/usr/local/lib/softhsm/libsofthsm2.so
# Apple Silicon:
export GLASSBOX_PKCS11_MODULE=/opt/homebrew/lib/softhsm/libsofthsm2.so

# OpenSC
brew install opensc
export GLASSBOX_PKCS11_MODULE=/usr/local/lib/opensc-pkcs11.so
# Apple Silicon:
export GLASSBOX_PKCS11_MODULE=/opt/homebrew/lib/opensc-pkcs11.so

# YubiKey (yubico-piv-tool)
brew install yubico-piv-tool
export GLASSBOX_PKCS11_MODULE=/usr/local/lib/libykcs11.dylib
# Apple Silicon:
export GLASSBOX_PKCS11_MODULE=/opt/homebrew/lib/libykcs11.dylib
```

### Windows

Windows PKCS#11 modules are typically `.dll` files. Common locations:

```powershell
# SoftHSM2 for Windows (https://github.com/disig/SoftHSM2-for-Windows)
$env:GLASSBOX_PKCS11_MODULE = "C:\SoftHSM2\lib\softhsm2-x64.dll"

# YubiKey Minidriver / YKCS11
$env:GLASSBOX_PKCS11_MODULE = "C:\Program Files\Yubico\Yubico PIV Tool\bin\libykcs11.dll"
```

> Note: Go's `plugin` package does not support Windows. PKCS#11 signing on
> Windows requires a cgo build with a native binding. The `--validate-only`
> flag will surface a clear error if the module cannot be loaded.

---

## SoftHSM2 setup (for testing)

SoftHSM2 is a software-only HSM useful for CI and local development.

```bash
# 1. Initialize a token
softhsm2-util --init-token --slot 0 \
  --label "glassbox-test" \
  --pin 1234 --so-pin 0000

# 2. Generate an Ed25519 key
pkcs11-tool --module /usr/lib/softhsm/libsofthsm2.so \
  --login --pin 1234 \
  --keypairgen --key-type EC:edwards25519 \
  --label glassbox-audit-key --usage-sign

# 3. Verify the key exists
pkcs11-tool --module /usr/lib/softhsm/libsofthsm2.so \
  --login --pin 1234 \
  --list-objects --type privkey

# 4. Run the preflight validator
export GLASSBOX_PKCS11_MODULE=/usr/lib/softhsm/libsofthsm2.so
export GLASSBOX_PKCS11_PIN=1234
export GLASSBOX_PKCS11_KEY_LABEL=glassbox-audit-key
glassbox audit:sign --hsm-provider pkcs11 --validate-only
```

---

## YubiKey PIV setup

YubiKey supports Ed25519 from firmware 5.7+. For older firmware use ECDSA P-256.

```bash
# 1. Generate a key in PIV slot 9c (Digital Signature)
ykman piv keys generate --algorithm ED25519 9c pubkey.pem

# 2. Create a self-signed certificate (required by PIV applet)
ykman piv certificates generate --subject "CN=Glassbox" 9c pubkey.pem

# 3. Set the PIN (default is 123456)
ykman piv access change-pin

# 4. Configure Glassbox
export GLASSBOX_PKCS11_MODULE=/usr/lib/x86_64-linux-gnu/libykcs11.so  # Linux
export GLASSBOX_PKCS11_MODULE=/usr/local/lib/libykcs11.dylib           # macOS
export GLASSBOX_PKCS11_PIN=<your-pin>
export GLASSBOX_PKCS11_KEY_LABEL="Private key for Digital Signature"

# 5. Validate
glassbox audit:sign --hsm-provider pkcs11 --validate-only
```

---

## Preflight validation

The `--validate-only` flag runs all PKCS#11 checks without signing any payload.
It is safe to run repeatedly and does not modify the token.

```
$ glassbox audit:sign --hsm-provider pkcs11 --validate-only

Running PKCS#11 preflight checks...

  [PASS] module_path    module file found: /usr/lib/softhsm/libsofthsm2.so (1234567 bytes)
  [PASS] module_load    module loaded successfully: /usr/lib/softhsm/libsofthsm2.so
  [PASS] slot_enum      using slot index 0 (slot ID 0, 1 slot(s) total)
  [PASS] token_info     token label: "glassbox-test" (slot 0)
  [PASS] session_open   session opened on slot 0 (handle 1)
  [PASS] pin_auth       PIN authentication successful
  [PASS] key_lookup     signing key "glassbox-audit-key" found (handle 42)
  [PASS] sign_test      test signing operation succeeded

Result: PKCS#11 configuration is valid and ready for signing.
```

When a check fails, the output includes a remediation hint:

```
  [FAIL] pin_auth       PIN authentication failed: pkcs11 C_Login failed (0x000000A0):
                        the PIN is incorrect — verify GLASSBOX_PKCS11_PIN is correct;
                        note that repeated failures may lock the token
```

---

## Troubleshooting

### Module file not found

```
[FAIL] module_path  module file not found: /usr/lib/softhsm/libsofthsm2.so
```

The path in `GLASSBOX_PKCS11_MODULE` does not exist. Check:
- The package is installed (`apt install softhsm2`, `brew install softhsm`, etc.)
- The path matches your OS architecture (x86_64 vs arm64, `/usr/lib` vs `/usr/lib64`)
- Run `find /usr -name 'libsofthsm2.so' 2>/dev/null` to locate the file

### No slots with tokens found

```
[FAIL] slot_enum  no slots with tokens found
```

The module loaded but no token is present. For SoftHSM2:

```bash
softhsm2-util --show-slots
# If empty, initialize a token:
softhsm2-util --init-token --slot 0 --label MyToken --pin 1234 --so-pin 0000
```

For a physical token, ensure it is inserted and recognized by the OS:

```bash
pkcs11-tool --module $GLASSBOX_PKCS11_MODULE --list-slots
```

### Token label not found

```
[FAIL] slot_enum  no token with label "MyToken" found (checked 2 slot(s))
```

`GLASSBOX_PKCS11_TOKEN_LABEL` does not match any token. Labels are case-sensitive.
List available tokens:

```bash
pkcs11-tool --module $GLASSBOX_PKCS11_MODULE --list-slots
```

### PIN incorrect

```
[FAIL] pin_auth  PIN authentication failed: the PIN is incorrect
```

- Verify `GLASSBOX_PKCS11_PIN` is set correctly
- Check for leading/trailing whitespace in the variable
- Repeated failures will lock the token; use the SO PIN to unlock:
  ```bash
  pkcs11-tool --module $GLASSBOX_PKCS11_MODULE --init-pin --so-pin 0000
  # YubiKey:
  ykman piv access unblock-pin --puk <puk>
  ```

### PIN locked

```
[FAIL] pin_auth  the token PIN is locked due to too many incorrect attempts
```

Unlock with the Security Officer (SO) PIN:

```bash
# SoftHSM2
pkcs11-tool --module $GLASSBOX_PKCS11_MODULE --init-pin --so-pin 0000

# YubiKey
ykman piv access unblock-pin --puk <puk>
```

### Key not found

```
[FAIL] key_lookup  signing key "my-key" not found
```

- List available private keys: `pkcs11-tool --module $GLASSBOX_PKCS11_MODULE --login --pin $GLASSBOX_PKCS11_PIN --list-objects --type privkey`
- Verify `GLASSBOX_PKCS11_KEY_LABEL` matches the `CKA_LABEL` exactly (case-sensitive)
- Ensure the key was created with `--usage-sign` / `CKA_SIGN=true`
- Ensure the key type is Ed25519 (`EC:edwards25519`); RSA and ECDSA P-256 are not supported

### Mechanism not supported (CKM_EDDSA)

```
[FAIL] sign_test  test signing operation failed: the CKM_EDDSA mechanism is not supported
```

The module does not support Ed25519 signing. Check:
- SoftHSM2 requires version 2.5.0 or later
- YubiKey requires firmware 5.2.3 or later for Ed25519
- Some enterprise HSMs require a separate license for EdDSA; consult vendor docs

### Permission denied loading module

```
[FAIL] module_load  failed to load PKCS#11 module: permission denied
```

The process user does not have read/execute permission on the module file:

```bash
ls -la $GLASSBOX_PKCS11_MODULE
# Fix:
sudo chmod o+r $GLASSBOX_PKCS11_MODULE
# Or add the user to the group that owns the file
```

### Module hangs on initialization

The validator enforces a 10-second timeout on module initialization. If the
module hangs (common with misconfigured smart card readers), the validator
will retry up to 2 times and then fail with a clear message. Check:
- The HSM device is connected and powered
- No other process holds an exclusive lock on the token
- The PKCS#11 middleware daemon (e.g. `pcscd`) is running:
  ```bash
  systemctl status pcscd
  systemctl start pcscd
  ```

### Signing error diagnostics

When a signing operation fails, Glassbox maps the raw PKCS#11 return code to a
human-readable message with actionable remediation. For example:

```
pkcs11 C_SignInit failed (0x00000070): the CKM_EDDSA mechanism is not supported
  — verify the module supports Ed25519 signing; SoftHSM2 requires version 2.5.0+;
  YubiKey requires firmware 5.2.3+
```

Common signing errors and their meanings:

| Error | Meaning | Action |
|-------|---------|--------|
| `CKR_MECHANISM_INVALID` | Ed25519 not supported | Update SoftHSM2 or check firmware |
| `CKR_PIN_LOCKED` | Token PIN locked | Unlock with SO PIN |
| `CKR_KEY_FUNCTION_NOT_PERMITTED` | Key lacks sign permission | Recreate key with `CKA_SIGN=true` |
| `CKR_SESSION_CLOSED` | Session expired | Reinitialize session |
| `CKR_DEVICE_REMOVED` | Token unplugged | Reinsert token and retry |

---

## Architecture Decision Records

The following ADRs govern the design decisions behind audit signing:

- [ADR-003: Trust Boundaries and Component Trust Levels](adr/003-trust-boundaries.md) — classifies the signing provider tier (Tier 3) and the controls that govern provider selection.
- [ADR-006: Provider Isolation](adr/006-provider-isolation.md) — documents the isolation properties of software, PKCS#11, and KMS providers; explains why only the hash digest crosses the provider boundary.
- [ADR-005: Canonicalization Ownership](adr/005-canonicalization-ownership.md) — specifies the canonical form and hashing algorithm whose output is passed to the signing provider.
- [ADR-007: Offline Guarantees](adr/007-offline-guarantees.md) — documents which providers work without network access and describes the air-gapped signing pipeline.
