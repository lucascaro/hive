# Signing and notarizing macOS releases

Hive's macOS releases are signed with an Apple Developer ID certificate,
notarized by Apple, and stapled so the ticket travels with the download. The
in-app updater refuses any release that is not signed by Hive's team.

This document is the one-time setup, plus what to do when it breaks.

## Why both halves matter

Signing alone is not enough. `codesign --verify` proves that *some* valid
Apple Developer ID signed a bundle — anyone who pays Apple $99 has one. What
makes the check mean "signed by Hive" is pinning the **Team ID**, which the
updater does in `cmd/hivegui/update_verify_darwin.go`.

Notarization is the other half: Apple scans the build and issues a ticket.
Stapling attaches that ticket to the bundle so Gatekeeper accepts it **offline**,
which is what removes the "unidentified developer" prompt on a fresh install.

## One-time setup

### 1. Create the Developer ID Application certificate

You need a paid Apple Developer account ($99/yr). The certificate is separate
from the account and has to be created by hand.

1. Open **Keychain Access** → menu **Certificate Assistant** → **Request a
   Certificate From a Certificate Authority**.
2. Enter your Apple ID email and name. Select **Saved to disk**, not "Emailed
   to the CA". Leave "CA Email Address" blank. Save the `.certSigningRequest`
   file.
3. Go to <https://developer.apple.com/account/resources/certificates/list> and
   click **+**.
4. Choose **Developer ID Application** — *not* "Mac Development", *not* "Mac App
   Distribution". Only Developer ID works for software distributed outside the
   App Store.
5. Upload the CSR from step 2, download the resulting `.cer`, and double-click
   it to install into your login keychain.

Confirm it landed:

```bash
security find-identity -v -p codesigning
```

You want a line reading `Developer ID Application: Your Name (TEAMID)`. The
10-character string in parentheses is your **Team ID**.

> If this prints `0 valid identities found`, the certificate is not installed.
> A common cause is downloading the `.cer` on a machine that does not hold the
> private key from the CSR — the certificate is only usable on the Mac that
> generated the request (or via an exported `.p12`).

### 2. Pin the Team ID in the source

Set it in `internal/buildinfo/signing.go`:

```go
var signingTeamID = "ABCDE12345"
```

This is a committed constant on purpose, not a build flag. Every build carries
it — including a credential-free `./build.sh` on another machine, and the
rebuild the GUI's latest-commit updater performs. If it arrived via `-ldflags`
at release time, all of those builds would carry an empty pin and would
silently skip signature verification on every release they later downloaded.

`scripts/release.sh` refuses to publish while this is empty.

Setting it also switches the updater's signature check on everywhere, tests
included: any test that drives `stageRelease` end to end must stub
`verifySignatureFn`, or it will shell out to `codesign` against an unsigned
fixture bundle and fail.

### 3. Store notarization credentials

Create an app-specific password at <https://appleid.apple.com> (Sign-In and
Security → App-Specific Passwords), then:

```bash
xcrun notarytool store-credentials hive-notary \
    --apple-id you@example.com \
    --team-id ABCDE12345
```

`store-credentials` prompts for the app-specific password and reads it without
echo. Do not pass it with `--password`: that puts the credential in your shell
history and exposes it in `ps` output for the duration of the call.

This writes to your login keychain. The password never appears in the repo, in
your shell history, or in a release command.

### 4. Export the two variables

```bash
export HIVE_SIGN_IDENTITY="Developer ID Application: Your Name (ABCDE12345)"
export HIVE_NOTARY_PROFILE=hive-notary
```

Put them in your shell profile. They are needed only for a **local** release
(`--local-artifacts`, below) — the normal path signs on a CI runner and needs
nothing in your environment.

## Releasing

```bash
./scripts/release.sh 0.5.0
```

That bumps the version, stamps the changelog from `.changesets/`, commits,
tags, verifies `origin/main` has not advanced, and pushes. **Then it exits**
and prints the Actions URL.

Pushing the `v*` tag triggers `.github/workflows/release.yml`, which on a
`macos-latest` runner builds every artifact, signs and notarizes the macOS
zip, writes the checksum manifest and creates the GitHub release. Nothing
further is required from you, and you need no certificate on the machine you
released from.

Budget **anywhere from 2 minutes to over an hour** for notarization — it is a
queue on Apple's side, not a build step. The v2.7.0 release sat `In Progress`
for over 90 minutes with nothing posted on
<https://developer.apple.com/system-status/>, which is exactly why this moved
off a human's terminal. The job's `timeout-minutes: 180` is the ceiling.

### If the run fails

The tag is already pushed, so the fix is to re-publish, not to re-tag:

```bash
gh workflow run release.yml -f tag=v0.5.0
```

`scripts/release-artifacts.sh` publishes idempotently — it updates an existing
release's assets with `gh release upload --clobber` rather than failing on
"release already exists" — so a re-dispatch repairs a half-finished publish.

### One-time CI setup

Seven repository settings, all under **Settings → Secrets and variables →
Actions**. Variables are not secret; secrets are.

| Kind | Name | What it is |
|---|---|---|
| Variable | `HIVE_SIGN_IDENTITY` | `Developer ID Application: Your Name (ABCDE12345)` |
| Variable | `HIVE_NOTARY_PROFILE` | Any name, e.g. `hive-notary`. The workflow creates the profile under it. |
| Secret | `MACOS_CERT_P12` | Base64 of the exported `.p12` (certificate **and** private key). |
| Secret | `MACOS_CERT_PASSWORD` | The password you set when exporting the `.p12`. |
| Secret | `NOTARY_API_KEY` | Base64 of the App Store Connect `AuthKey_<KEYID>.p8`. |
| Secret | `NOTARY_KEY_ID` | The key's 10-character ID. |
| Secret | `NOTARY_ISSUER_ID` | The issuer UUID from App Store Connect. |

**Export the certificate.** In Keychain Access, select the *Developer ID
Application* certificate **with its private key** (expand the triangle and
select both rows), right-click → Export → `.p12`, and set a password. Then:

```bash
base64 -i certificate.p12 | pbcopy    # paste as MACOS_CERT_P12
```

**Create the notary key.** App Store Connect → Users and Access → Integrations
→ App Store Connect API → generate a key with the **Developer** role. The
`.p8` downloads exactly once. Then:

```bash
base64 -i AuthKey_XXXXXXXXXX.p8 | pbcopy   # paste as NOTARY_API_KEY
```

An API key rather than an app-specific password because it is revocable on its
own, without touching anyone's Apple ID, and it does not expire on a password
rotation.

### Releasing entirely locally

The old behaviour, kept as a fallback for when CI is unavailable:

```bash
./scripts/release.sh 0.5.0 --local-artifacts
```

This needs `HIVE_SIGN_IDENTITY` and `HIVE_NOTARY_PROFILE` exported and the
certificate in your keychain, and it blocks your terminal through
notarization. It runs the same `scripts/release-artifacts.sh` the workflow
does, so the two paths cannot drift.

It pushes the release **commit** but deliberately not the tag: the remote tag
is created by `gh release create --target` as part of publishing. That
ordering is what keeps the two paths from racing — by the time the tag
appears and triggers `release.yml`, a complete release already exists, and
the workflow's first step sees all three assets and stands down. Push the tag
yourself and you get two macOS builds notarizing and clobbering the same
assets at once, which `--clobber` would make look like success.

The stand-down check requires **all three** assets. A release left partial by
a run that died mid-publish is republished rather than skipped — that is the
case `gh workflow run release.yml -f tag=<tag>` exists to repair.

## Testing hardened runtime without a certificate

Notarization requires the hardened runtime, which restricts what a process may
do at runtime and can break things like a PTY host or a login item. You do not
need a certificate to find that out:

```bash
./build.sh --platform macos
scripts/sign-macos.sh --adhoc cmd/hivegui/build/bin/hivegui.app
open cmd/hivegui/build/bin/hivegui.app
```

An ad-hoc signature still sets the hardened-runtime flag
(`codesign -dv` reports `flags=0x10002(adhoc,runtime)`), so the runtime
restrictions are real. Start a session and confirm the PTY spawns a shell and
`hived` stays up.

An ad-hoc signature carries no Team ID, so a bundle signed this way can never
pass the updater's pin. It is a development tool, not a distribution path.

## Verifying a release

```bash
ditto -x -k Hive-0.5.0-macos-universal.zip /tmp/check
codesign --verify --deep --strict --verbose /tmp/check/hivegui.app
xcrun stapler validate /tmp/check/hivegui.app
```

To reproduce what a downloading user actually faces, add the quarantine
attribute and pull the network — a locally extracted bundle has no quarantine
flag, so `spctl` on it does **not** prove the ticket is stapled:

```bash
xattr -w com.apple.quarantine "0083;00000000;Safari;" /tmp/check/hivegui.app
# disable networking, then:
spctl --assess --type execute --verbose /tmp/check/hivegui.app
```

A pass with the network down is the real proof the ticket is stapled rather
than fetched from Apple on demand.

## Rotating the key

Deliberately manual — there is one key, and automating rotation for a set of
one is machinery with no second case to justify it.

1. Create a new Developer ID Application certificate (steps above). Apple
   allows more than one to exist at a time, so do this **before** revoking the
   old one.
2. Update `signingTeamID` in `internal/buildinfo/signing.go` **only if the Team
   ID itself changed**. Rotating a certificate within the same account keeps
   the Team ID, so usually no code change is needed — that is the main
   advantage of pinning the team rather than a certificate fingerprint.
3. Update `HIVE_SIGN_IDENTITY` to the new certificate's full name.
4. Cut a release and verify it as above.
5. Revoke the old certificate in the Apple developer console.

Already-notarized releases keep working after a revocation: the notarization
ticket is what Gatekeeper checks, and it is not invalidated by revoking the
signing certificate.

**If the Team ID does change** (a new Apple account, an org migration), every
already-installed client pins the old one and will refuse the first update
signed with the new team. Those users must reinstall by hand. Plan a release
that warns first.

## Troubleshooting

| Symptom | Cause |
|---|---|
| `0 valid identities found` | Certificate not installed, or installed on a Mac without the CSR's private key. |
| `Team ID mismatch` from `release.sh` | `HIVE_SIGN_IDENTITY` and `signingTeamID` disagree. Fix whichever is wrong — publishing with a mismatch produces a release no client can install. |
| `notarytool` returns `Invalid` | Run `xcrun notarytool log <submission-id> --keychain-profile hive-notary` for the per-file reason. Usually an unsigned nested binary or a missing hardened runtime. |
| `The staple and validate action failed` | The bundle was re-signed after stapling. Signing invalidates a ticket — staple last, and never re-sign afterwards. |
| Notarization fails **in CI** | The tag is already pushed. Never reset or delete it — fix the cause and re-publish with `gh workflow run release.yml -f tag=<tag>`. Publishing is idempotent, so repeating it is safe. |
| Notarization fails during `--local-artifacts` | The release commit is on `origin` but no tag exists yet (the tag is created when the release is published). Fix the cause and re-run `scripts/release-artifacts.sh <version>`. Do **not** reset the commit or re-run `release.sh`: the commit is already published, so a re-run would re-bump the version and re-stamp the changelog on top of it. |
| `codesign` hangs in CI with no output | A keychain step was dropped. All four of `unlock-keychain`, `set-key-partition-list`, `list-keychains -d user -s` and `default-keychain -s` are required; the workflow comments explain what each one prevents. |
| `refusing to publish unsigned` | `HIVE_SIGN_IDENTITY` / `HIVE_NOTARY_PROFILE` are not reaching `release-artifacts.sh`. In CI, check the repo **variables** (not secrets). This refusal is deliberate — publishing unsigned would produce a release every client's updater pin rejects. |
