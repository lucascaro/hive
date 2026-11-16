---
title: Agent-control credential and switch
stage: TRIAGE
---

# Agent-control credential and switch

- **Issue:** none (local spec)
- **Type:** enhancement
- **Complexity:** to be set at triage
- **Priority:** to be set at triage
- **Exec plan:** none yet

## Problem

Any process running as the user can connect to the daemon's full control
socket, and an agent with a shell is such a process. Through that socket
it can create sessions with any command, type into any session, answer
permission prompts, approve plans and install plugins. The per-session
*May direct other sessions* grant (spec 389) is enforced only on the
narrowed session socket, so it limits agents that use Hive's messaging
honestly and nothing else. One prompt-injected agent can therefore drive
every session, and a user who reads the grant as a boundary is wrong to.
[SECURITY.md › Trust model](../../SECURITY.md#trust-model) documents this
today. This spec is about closing it.

Verified on `main` at the time of writing:

- Control socket and its events sibling are plain Unix-domain listeners
  with no peer authentication — `internal/daemon/daemon.go:337`, `:343`;
  path from `internal/daemon/socket.go:44` (`SocketPath`), events path is
  the control path plus `.events` (`socket.go:173`), and that events path
  is what a session receives as `HIVE_SOCKET`.
- The only cross-user protection is the directory check
  (`socket.go:195`, `CheckSocketDir`), a no-op on Windows.
- Control verbs reachable by any connecting client: `CREATE_SESSION`
  (`daemon.go:1660`), input to any session through an attach
  (`daemon.go:2462`), `ANSWER_PERMISSION` (`daemon.go:1900`),
  `RESOLVE_PLAN_REVIEW` (`daemon.go:2303`), plugin install/enable/remove
  (`daemon.go:2351`). Plugins get the same modes
  (`internal/daemon/plugins.go:179`).
- The grant is checked only for `SEND_TO_SESSION` from a session
  connection (`daemon.go:1379`, `internal/registry/deliver.go:445`).

## Desired behavior

**(a) The control socket accepts only Hive's own clients.** The GUI, the
menu bar (`hivebar`), and the `hived` CLI *when the user runs it* can
open a control connection. Any other process, including an agent and
anything it launches, cannot. This rests on a credential agents cannot
read. The candidate is a macOS Keychain item whose access is limited to
signed Hive binaries, so that an agent trying to read it gets a system
prompt or an error, never the secret. Research so far (see *Notes*)
says the Keychain can do the "limited to signed Hive binaries" part but
cannot by itself do the "when the user runs it" part; that gap is the
first open question below and may reshape (a).

**(b) Once (a) is enforced, a setting turns agent-to-agent control
off.** Settings gains a switch that disables agent-to-agent control
entirely: no session may direct, message or observe another through
Hive. Default **on**, which is today's behaviour. The switch ships only
after (a), by the owner's decision: a switch an agent can route around
is worse than none, because it tells the user they are protected when
they are not.

The narrowed per-session socket keeps working for a session's own
children (`hive idea`, state reports, `submit_result`) unless an open
question below decides otherwise.

## Open questions

1. **Telling the user's `hived` CLI from an agent's.** The daemon and
   the CLI are one binary, `Contents/MacOS/hived`, signed once
   (`scripts/sign-macos.sh:70`). A Keychain ACL or access group trusts a
   code signature, not whoever launched it, so any rule that lets the
   user's `hived msg` read the credential also lets an agent that runs
   the same binary read it — silently. Candidates:
   - split the CLI into its own binary that is *not* trusted, so it has
     no control access and talks only through the narrowed socket or a
     GUI-mediated request;
   - require user presence for CLI control (Touch ID / password through
     `SecAccessControl` `.userPresence`), which needs the data
     protection keychain (see *Notes*) and prompts on every use;
   - route privileged CLI verbs through a confirmation the GUI shows,
     so the human approves each one.
   Every code path in a trusted binary that can reach the credential is
   reachable by an agent that can run that binary; the design has to
   hold under that.
2. **Unsigned, dev and ad-hoc builds.** `wails dev`, `go run` and
   `scripts/sign-macos.sh --adhoc` builds have no Team ID and change
   signature on every build. Options: no credential (today's behaviour)
   with a visible "unprotected build" marker; a per-build ACL entry that
   prompts after each rebuild; or a dev-only escape hatch. Whatever is
   chosen must not be reachable by a release build.
3. **Linux and Windows.** Neither platform has a per-application secret
   store that same-user processes cannot read. GNOME Keyring's Secret
   Service lets any process on the session bus read an unlocked
   collection (CVE-2018-19358), and DPAPI's current-user scope decrypts
   for any process running as the user. Candidates: declare (a) and (b)
   macOS-only and say so in the UI; or look for a different mechanism
   per platform (for example a peer-credential check combined with an
   executable-identity check). Deciding "out of scope" is an acceptable
   answer if it is explicit.
4. **The credential must never travel through env or argv.** A same-user
   process can read another process's argv, and for non-platform
   binaries its environment, through `sysctl(KERN_PROCARGS2)` (observed
   on macOS 26.7.1: environment hidden for Apple platform binaries,
   visible for a Homebrew `python3`). The daemon must obtain the
   credential itself (or generate it and hand it out only over an
   authenticated channel), and no client may pass it on a command line
   or in an environment variable — including the GUI spawning the
   daemon.
5. **A session's own children and the narrowed socket.** Does the
   narrowed socket stay open to whatever a session spawns (it carries
   `hive idea`, state hooks, `submit_result`, and `hived msg` for a
   granted session)? If yes, (b) must at least refuse `SEND_TO_SESSION`,
   `LIST_SESSIONS` and `WAIT_SESSION` there when the switch is off. If
   no, those integrations need another path.
6. **Other routes around the socket.** A same-user agent can edit files
   Hive trusts on restart — custom agent commands in `agents.json`,
   registry state, the plugins directory — and other agents' own config
   (for example Claude Code hooks in `~/.claude/settings.json`). (b)
   must either cover the Hive-owned ones (so the switch cannot be undone
   by editing state while the daemon is down) or list them as explicit
   non-goals in the UI copy. Another agent's config is outside Hive's
   reach and stays a non-goal.
7. **Migration.** Existing installs have no credential; the first
   release with (a) must create it without breaking a running daemon,
   an older GUI talking to a newer daemon (a `buildinfo.DaemonContract`
   question), or users who script against the control socket today.
   What does a refused client see, and how does a user who relied on
   scripting get it back?

## Success criteria

Each is checkable by someone who did not build it, on a release-signed
macOS build:

- An agent process that runs `security find-generic-password` (or any
  Keychain API) for the credential item gets a system prompt or an
  error, never the secret.
- A process that is not a Hive client — for example `nc -U` or a short
  script dialing the control socket — is refused before any control
  verb is served, and the daemon log records the refusal.
- Running the shipped `hived` binary from inside an agent session does
  not unlock any control verb without the user's explicit approval.
- The GUI and `hivebar` keep working with no prompt during ordinary use.
- With the agent-to-agent switch off, `hived msg` from a granted
  session is refused, and so is the same request sent any other way an
  agent can reach the daemon.
- No credential appears in any Hive process's argv or environment (check
  with `ps -E -ww` / `KERN_PROCARGS2`).

## Non-goals

- Sandboxing agents or limiting what they can do *outside* Hive (files,
  network, other apps). Agents still run as the user.
- Protecting against malware already running as the user with the
  ability to drive the GUI (Accessibility / AppleScript permissions) or
  a compromised OS.
- Changing the per-session grant model of spec 389, or how a session's
  identity is established on the narrowed socket.
- Protecting other agents' own configuration files.
- A per-plugin permission model.

## Notes

Research (2026-11):

- Apple, *Access Control Lists* — file-based keychain items carry an ACL
  of operations and trusted apps; an untrusted caller makes the system
  prompt, and **"Always Allow" adds that app to the trusted list
  permanently**. One click therefore turns the prompt into silent
  access — e.g. allowing `/usr/bin/security` once lets every later
  caller of `security` read the item. Any design that relies on the
  prompt must say what the prompt shows and what the user should click.
  https://developer.apple.com/documentation/security/access-control-lists
- Apple, *TN3137: On Mac keychain APIs and implementations* — macOS has
  the file-based keychain (ACLs) and the data protection keychain
  (keychain access groups from code-signing entitlements). Access-group
  entitlements "must be authorized by a provisioning profile" and need an
  app-like bundle, which `hived` (a bare executable inside the app) does
  not have today. The `security` tool "is primarily focused on the
  file-based keychain". The file-based keychain is "on the road to
  deprecation". https://developer.apple.com/documentation/technotes/tn3137-on-mac-keychains
- Apple, `kSecAttrAccessGroup` / `kSecUseDataProtectionKeychain` /
  `errSecMissingEntitlement` — an item in an access group is visible only
  to code whose entitlements include that group; another process gets
  "not found" or a missing-entitlement error rather than a prompt. On
  macOS the attribute applies only with the data protection keychain.
  https://developer.apple.com/documentation/security/ksecattraccessgroup ·
  https://developer.apple.com/documentation/security/ksecusedataprotectionkeychain ·
  https://developer.apple.com/documentation/security/errsecmissingentitlement
- What `security` can read without a prompt follows from the ACL rule
  above: for `security find-generic-password -w`, the caller is
  `/usr/bin/security`, so it prompts unless the item's ACL lists it (as
  `security add-generic-password -T /usr/bin/security` would).
  Attributes other than the secret (service, account, label) are listed
  without a prompt. Not yet verified hands-on against a Hive-created
  item; doing so is part of triage.
- freedesktop Secret Service spec leaves access control as TODO; CVE
  2018-19358 (GNOME Keyring: local users read secrets over D-Bus when
  unlocked). https://specifications.freedesktop.org/secret-service/latest/ch10.html ·
  https://access.redhat.com/security/cve/CVE-2018-19358
- Microsoft, `CryptProtectData` — current-user DPAPI data can be
  decrypted by any process with the same logon credentials.
  https://learn.microsoft.com/en-us/windows/win32/api/dpapi/nf-dpapi-cryptprotectdata

**Conclusion of the research.** The Keychain *can* keep the secret from
processes that are not signed Hive binaries: a file-based item with a
Hive-only ACL prompts any other reader, and a data-protection item in a
Hive access group is invisible to them. It *cannot* tell the user
running `hived` from an agent running `hived`, because both are the same
signed binary — so (a) holds only if no agent-runnable Hive binary hands
out control without the user's approval (open question 1). The
data-protection route is the stronger one (no "Always Allow" to click
through) but needs a provisioning profile and an app-like bundle for
every client that reads it. Alternatives if neither fits:

- **Peer identity instead of a secret.** The daemon reads the
  connecting peer's audit token / PID (`LOCAL_PEERTOKEN`,
  `LOCAL_PEERPID`) and checks its code signature against Hive's
  designated requirement. No secret to steal, but the same CLI caveat
  applies, and PID-based checks race.
- **GUI-only control.** Only the GUI holds control; `hivebar` and the
  CLI go through it, and every CLI-originated privileged request needs a
  click in the GUI.
- **Separate user / sandbox as the boundary** (today's advice in
  SECURITY.md), with (b) never shipping.

Related: [agent-orchestration.md](../design-docs/agent-orchestration.md),
spec 389 (orchestrator grant), [daemon-contract.md](../design-docs/daemon-contract.md).
