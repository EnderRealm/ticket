# Verify policy

`tk verify <id>` and the `ticket_verify` MCP tool run the commands a ticket's
acceptance criteria declare. A verify command is content: agents write ticket
bodies, and bodies replicate to every machine over the shared store's git
remote. This page is the policy that keeps that content from becoming
arbitrary code execution. `tk verify --help` covers the flags and criterion
syntax; the README's
[Verifiable Acceptance Criteria](../README.md#verifiable-acceptance-criteria)
section has the full reference, including the record format and the MCP tools.

## Where commands run

Each command runs in the checkout registered on this machine for the ticket's
own project, and only there. Eligibility is decided before anything runs:

- A Root ticket is refused, since Root has no repository.
- A ticket in a project with no checkout registered on this machine is refused.
- A ticket whose registered checkout is not a directory here is refused.

The working directory, `HOME`, a parent epic's project and the ticket store
never stand in, and nothing is recorded for a refused run. `--dir` moves an
eligible run and applies only after that check; it makes nothing eligible.

### The `implementation` field

A ticket tracked in one project but implemented in another names that project:
`tk edit <id> --set implementation=<project>`. Its commands then run in the
checkout registered here for that project instead, under that project's
`verify_timeout`, and the run is recorded as `implementation <project>` beside
the checkout. The field names a project, never a path; one that does not
resolve to a registered checkout here is refused, never falling back to the
ticket's own. Remove the field (`--set implementation=`) to verify in the
ticket's own checkout again.

## No shell

Commands are execed as argv and never through a shell: quotes group
arguments, and everything else (`;`, `|`, `&&`, `$()`, backticks, `~`) is
literal text handed to the program.

## `verify_allow`

A command runs only if its program — the first word — exactly matches an entry
in `verify_allow` in `~/.ticket/config.yaml`; anything else is refused without
running. Only that machine-local file grants permission: the shared
central-store config, ticket content and MCP arguments cannot, so an agent can
never widen the list itself. `TK_STORE_ROOT` does not move it either. An empty
`verify_allow` refuses everything, and so does a local config that cannot be
parsed.

The default, when the key is absent, is `go`, `make`, `cargo`, `pytest`.

Listing a program trusts whoever writes verify lines with everything that
program can do — including `go run pkg@version`, `go install pkg@version`,
`cargo install <crate>` and `make -f <file>`, which run code from outside the
repo. Shells and interpreters are absent from the default. `swift` is not in
it because `swift -e <code>` runs arbitrary Swift, so a Swift project adds
`swift` to `verify_allow` itself to get `swift test` back.

## Launcher prefix

`tk verify --launcher <word>`, repeated once per argv word, execs each command
as that prefix followed by the command's own argv — `sudo -n -u <agent user> -H
env -i …`, say — so the command runs with the launcher's privileges while tk
reads the store and `verify_allow` with its own. The prefix is the caller's
and is trusted like `--dir`: it is checked against nothing. `verify_allow` is
still checked against the criterion's own program before the prefix is added,
so a prefix cannot make a refused command run, even when its own program is
listed. It is a command-line flag only: `ticket_verify` has no argument for
it, because a model calling the tool must never choose a uid or a wrapper, and
no ticket field or config key supplies one. `--check` reports the argv a run
would exec, prefix included.

A launcher changes how a run is stopped. A command under sudo runs as a user
tk cannot signal, and sudo cannot relay SIGKILL, so on timeout or cancellation
tk sends SIGTERM to the launcher's process group, which sudo relays, then
SIGKILL to that group 2s later, inside the 5s tk waits for output before
giving up on the process. An unprefixed command's group is still sent SIGKILL
at once. A process that leaves the launcher's group, or that the launcher does
not relay to, is reached by neither signal. The sequence is checked on macOS
against a real `sudo -n -u <agent user>` prefix; on Linux, where sudo relays
SIGTERM the same way, it is reasoned about rather than tested.

## `verify_timeout`

Each command is bounded by `verify_timeout` under the project in
`~/.ticket/config.yaml`, beside its `path`: a Go duration such as `300s` or
`5m`, defaulting to `120s` when unset. A timed-out command's output names the
bound that was applied and whether it came from `verify_timeout` or from the
default.

It is machine-local like `verify_allow` — the bound is a property of the
machine and the suite, not of a ticket that syncs to every machine — and a
value that is not a positive duration refuses every verify command in that
project rather than falling back to the default.

## When settings are read

Both settings are read from `~/.ticket/config.yaml` at the start of every run,
by `tk verify` and by `tk serve` alike, so an edit applies to the next run with
no server restart. The staleness that does bite is a `tk serve` process older
than the build that introduced a setting: a binary predating `verify_timeout`
applies the 120s it was built with however current the config is, so restart
the server after upgrading.

`tk verify --policy` prints both as they would be enforced right now — the
allow-list and the resolved project's `verify_timeout` (120s when unset) — so
verify lines can be written against it; `--json` for a script.
`ticket_store_info` returns the same as `verify_policy`. An unreadable config is
reported as the error with no list, never as the defaults.

## Results and output

A criterion's indented continuation line says who decides it. `verify:
<command>` is run by tk. `test: <what to look at>` is checked by a person on the
real build: nothing is run for it, and it is reported waiting. A criterion with
neither is judged by the run's review and its tests, and is reported
unverified; an `unverifiable: <reason>` line is retired and reads the same way,
its reason kept. On a criterion carrying both `verify:` and `test:` the `test:`
line wins and the command is not run.

Results are recorded in the ticket's Test Results section, each failing
criterion followed by its indented output; the exit status is non-zero on any
failure or refusal, and neither an unverified nor a waiting criterion makes it
so. With `--criterion` it grades that one criterion: 0 pass, 1 fail, 20 refused,
unverified or waiting. A command's
output is capped to its last 4KB, where test frameworks print failures, and a
capped output opens with a line naming how many bytes were dropped from the
head.

## MCP `dir` versus CLI `--dir`

`--criterion`, `--no-record` and `--launcher` are CLI-only, and the
`ticket_verify` MCP tool's `dir` is narrower than `--dir`: it accepts only the project's configured
checkout or a linked git worktree of that repository, refuses anything else
before running, and always records. An MCP caller's arguments are shaped by
ticket content, and a sandboxed client with no shell would gain reach it does
not otherwise have; a CLI caller already has a shell and can run anything
anywhere, so `--dir` is used as given and widens nothing for it. Nothing widens
`verify_allow`.

## `acceptance_id`

`ticket_criteria` returns the parsed criteria with `acceptance_id`, the
contract's identity. `ticket_verify` refuses a stale `acceptance_id` before
running and does not record a run whose criteria changed meanwhile. The report
and the Test Results record name the directory the commands ran in, the
`acceptance_id` and the caller's `candidate`.

## Long-running MCP runs

MCP verification waits up to 10 seconds, then returns a job ID for
`ticket_verify_status` polling. Request timeouts do not cancel commands;
`ticket_verify_cancel` or a session disconnect does. Status recovers a lost
start response by ticket ID without running the commands again.
