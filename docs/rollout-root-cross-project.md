# Rollout: Root and cross-project epics

Runbook for activating `root-namespace` and `cross-project-parents` on a shared central store, and for rolling back. Design: [warp/docs/root-tickets-cross-project-epics.md @ e4d93fb](https://github.com/EnderRealm/warp/blob/e4d93fb/docs/root-tickets-cross-project-epics.md). The procedure is executed once per store; its evidence goes in the [release record](#release-record).

Out of scope: multi-repo automatic execution of an epic, and identity-preserving moves between namespaces. `tk move` remains copy-and-close for isolated leaves.

## What activation changes

`tk activate` (see `tk activate --help`) is the only write. It:

- catalogues every namespace in `catalog.yaml` that is not yet listed: `_root` as `root`, a registered project as `project`, any other directory under `tickets/` as `legacy`. Existing entries keep their kind.
- writes a `.namespace` marker into every namespace directory, creating `tickets/_root/`.
- adds the features to `required_features`, last.

It never rewrites, moves or deletes a ticket file, never registers a repository, and never parents anything. Membership is changed only by the reviewed [adoption](#adoption) after activation.

It refuses without writing anything, and names every cause, on any of these:

- a catalog that cannot be read or parsed, or one that requires a feature the running tk lacks
- a requested `--feature` the running tk does not implement
- no `tickets/` directory
- a `_root` config entry, or a `_root` directory that is not catalogued as Root
- a catalogued namespace with no directory
- a namespace path that is not a real directory, or a marker that is not a regular file
- when turning on `cross-project-parents`: any ticket that already names a parent in another namespace (the feature would adopt it with nobody reviewing it), or an unreadable ticket file, which could be one

## Compatibility facts

- **Guard.** Every tk from v8.6.0 on checks `required_features` before every write. A binary missing a required feature refuses every write and says to replace it. Reads are never gated.
- **Features claimed.** Every guarded tk (v8.6.0 and later) lists both `root-namespace` and `cross-project-parents` in `ticket.SupportedFeatures`. So the guard does not refuse v8.6.0–v8.12.x against an activated store. Whether those versions are acceptable is a release decision. The minimum version recorded in the release record is the one that carries `tk activate`, and every process has to be at or above it.
- **Pre-guard binaries (before v8.6.0)** ignore `catalog.yaml` and the markers entirely. Nothing written to the store can make them refuse. They have to be absent from every machine and process before activation. Running one against an activated store is unsupported.
- **Embedded library.** Installing a new `tk` does not update a Go program that embeds `github.com/EnderRealm/ticket/v8`. loom and shuttle both embed it, and each has to be rebuilt against the release and reinstalled. `go version -m <binary>` shows the version a binary embeds.
- **Long-lived processes** keep the code they started with: `tk serve` (spawned by every MCP client, and by weft), `tk watch run` (the watcher), and any loop that runs `tk sync`. Each one has to restart after its binary is replaced.

## Procedure

Run the steps in order. Do not start a step until the previous step's evidence is recorded.

### 1. Backup

On the activating machine:

```bash
tk sync
git -C <central_root> status --porcelain
git -C <central_root> rev-parse HEAD
git -C <central_root> bundle create <backup_dir>/tickets-store-<date>.bundle --all
git -C <central_root> bundle verify <backup_dir>/tickets-store-<date>.bundle
```

`status --porcelain` must print nothing. Record the HEAD SHA and the bundle path. A second person, or a fresh session that did not produce the bundle, reviews that the bundle verifies and that its HEAD matches.

### 2. Audit

```bash
tk audit
tk audit --json > <evidence_dir>/audit-before.json
```

Resolve, or explicitly accept in the release record:

- `unreadable`: a file that does not parse. This blocks `cross-project-parents`. Repair it or remove it.
- `parent-cross-project`: a stored parent in another namespace. This blocks `cross-project-parents`. Clear each such parent (`tk --project <ns> edit <id> --parent ""`) and put the intended ones on the adoption list.
- Epics with outgoing deps: move each real requirement onto an acceptance leaf. Epics never read their own deps.
- Duplicate IDs, and files whose `id` names another project.

Content findings (`empty-acceptance` and similar) do not block activation.

### 3. Inventory

For every machine that clones the store, and every unprivileged agent user (`weftagentN`) on it, record:

```bash
which -a tk
tk --version
go version -m "$(which tk)" | head -3
go version -m "$(which loom)" | grep EnderRealm/ticket
go version -m "$(which shuttle)" | grep EnderRealm/ticket
git -C ~/code/warp rev-parse HEAD                  # rendered warp source
pgrep -fl 'tk serve'                               # MCP servers (agent clients, weft)
pgrep -fl 'tk watch'                               # journal watcher
tk watch status
```

Also record:

- the weft build in use, and the `tk` on the PATH weft spawns `tk serve` from
- every scheduled or looping `tk sync` (launchd, cron, a harness)
- every machine that clones the store remote (`git -C <central_root> remote -v` names the remote, but the list of clones has to come from the people who own them)

### 4. Upgrade and restart

On every inventoried machine and agent user:

1. Install the release: `brew upgrade ticket`, or the release archive.
2. Rebuild and reinstall loom and shuttle against it (`go get github.com/EnderRealm/ticket/v8@vX.Y.Z`, then build and install).
3. Re-render warp (`adapters/*/render.sh`).
4. Restart every long-lived process:
   - `tk watch stop`, then `tk watch start`
   - restart or reconnect every MCP client so each `tk serve` respawns
   - relaunch weft
   - restart any sync loop
5. Re-run the step 3 commands. Every binary has to be at or above the minimum, and every listed process has to have started after its install:

   ```bash
   ps -o pid,lstart,command -p <pid>
   ```

No feature is active yet. Every process now runs a guarded binary that implements both features.

### 5. Preflight

```bash
tk sync
tk activate --dry-run
tk activate --dry-run --json > <evidence_dir>/activate-plan.json
```

Review the planned `namespaces catalogued` list and its kinds. Every directory under `tickets/` becomes a namespace, throwaway smoke directories included. Rename or remove a directory now if it should not be one, because activation removes nothing. A refusal lists every cause. Fix them and re-run the dry run until it reports a plan.

### 6. Activate

On one machine only:

```bash
tk activate
tk sync
git -C <central_root> show --stat HEAD
```

The commit must contain only `catalog.yaml` and `.namespace` files. If a `.md` file appears in it, stop and restore from the bundle. A comment in `catalog.yaml` that described the preactivation state is kept as written, so edit it by hand in a separate commit if it is now stale.

### 7. Propagate and confirm

On every other machine:

```bash
tk sync
tk activate --dry-run
```

The dry run must report `already activated`. That shows the machine has the activation commit and runs a binary that honours it. Then:

```bash
tk activate --dry-run --json
tk audit --json > <evidence_dir>/audit-after.json
```

`inventory.complete` in the JSON must be `true`. Compare `audit-after.json` with `audit-before.json`: activation itself adds no finding.

### 8. Cross-tool walkthrough

Run these checks with the installed tools, not the test suite, and record each result:

- [ ] **Root capture.** `tk --project _root create "<idea>"` from outside any repo, and `ticket_create(project="_root")` over MCP, both land as `_root/<id>`. The id shape is unchanged.
- [ ] **Project epic with foreign children.** Create an epic in one project and a child in another with `--parent <project>/<epic-id>`. `ticket_show` lists both children with their namespaces, and `ticket_list(parent=…, project=<other>)` narrows the rows without changing the epic's `children_total`.
- [ ] **Complete rollup.** All children done gives the epic `done`, with `closed` equal to the latest child completion. Reopening a foreign child gives `open` with no `closed`. The tk CLI (`tk query`), MCP, the weft board, loom and shuttle all agree.
- [ ] **Degraded rollup.** With an unreadable file in any namespace, the epic reads neither done nor closed, `complete: false` names the file, and weft, loom and shuttle show it as incomplete rather than done.
- [ ] **Child repository routing.** weft and warp `/work` launch each child in its own project's registered checkout and never in the epic's. A child whose project has no checkout on this machine is blocked visibly.
- [ ] **No Root verification or run.** `tk verify` (with or without `--dir`), `ticket_verify`, the TUI spawn, weft launch and warp `/work` all refuse a Root leaf, naming that Root has no repository.
- [ ] **Local-only auto-close and retrospect.** In repo A, a commit that names `Closes: <B>/<id>` or `Closes: _root/<id>` closes nothing. A commit closing an A ticket closes it once, and `auto_retrospect` fires once for it. Historical closes do not re-extract.

### 9. Adoption

Adoption turns existing contextual links into parents. It is done only after step 7, and only from a reviewed, explicit list:

| Epic (qualified) | Candidate leaf (qualified) | Current parent | Current link? | Decision: parent / keep as link | Reviewed by |
|---|---|---|---|---|---|
| | | | | | |

Rules:

- Only a row decided `parent` is applied: `tk --project <leaf-ns> edit <leaf-id> --parent <epic-ns>/<epic-id>`, or `ticket_edit(id, parent)`. A leaf that already has a parent stays there unless its row says to move it.
- IDs and homes are unchanged. A leaf stays in the project that delivers it, and an epic stays where it is.
- Links are left exactly as they are, the adopted ones included. A link marked contextual is not a membership candidate. Of the seven links on `loom/close-knowledge-work-0c1a`, the six members its description names are candidates and the seventh is contextual.
- External members of the tracking epic for this rollout are attached only now, after support is active, from the same list.
- Afterwards, `ticket_show(<epic>)` lists exactly the adopted leaves plus the existing children, `tk audit` reports nothing new, and every adopted leaf's file differs only in `parent:`. Check that with `git -C <central_root> show HEAD`.

## Rollback

### Before activation (steps 1–5)

Nothing in the store has changed, so rollback is a binary rollback: reinstall the previous tk, loom, shuttle and weft builds and restart their processes. The catalog does not require any feature yet, so every version reads and writes the store as before.

### After activation (step 6 on)

Replacing binaries alone is not a rollback:

- A guarded binary that lacks a required feature refuses every write and keeps reading. The fixture `TestBinaryRollbackAcrossActivation` covers this.
- A guarded binary from v8.6.0–v8.12.x claims both features, so it is not refused. It is supported only if the release record's minimum allows it.
- A pre-guard binary (before v8.6.0) is not refused at all, and can read and write an activated store wrongly. It must not be reinstalled.
- Deleting the `required_features` line is not a data rollback. Root tickets become unwritable, and foreign-parented leaves stop being children and become relationship issues. The data that relied on the feature stays where it is.

There are two supported options:

1. **Retain compatible readers.** Keep the store activated and keep every binary at or above the minimum. Fix forward. This is the default.
2. **Reviewed reverse migration.** It is planned and reviewed like adoption, and runs in this order:
   1. Take a new backup (step 1).
   2. List every foreign child. Use `ticket_list(parent=<epic>, all_projects=true, include_closed=true)` for each epic, or `tk query --all-projects` and compare each leaf's parent namespace with its own.
   3. For each foreign child, reparent it to a local epic or clear its parent. Record a contextual link where the relationship should stay visible.
   4. Handle every Root ticket. Close it, or recreate it in a project with `tk move` (a new ID; isolated leaves only). No identity-preserving move exists.
   5. Run `tk audit` and confirm no foreign parent and no open Root ticket remains.
   6. Remove the features from `required_features` in a reviewed commit. Leave the namespace entries and markers, which preactivation readers ignore.
   7. `tk sync` everywhere, then roll binaries back as in the preactivation case.

## Consistency

There are no distributed transactions. The store lock is per machine. It orders writes and snapshots on one host and nothing across hosts. The store reaches other machines through git, which is eventually consistent:

- The activation commit, the binary upgrades and the process restarts are separate actions on separate machines. Nothing makes them atomic. That is why the procedure upgrades everything first, activates on one machine, then syncs.
- Until a machine has pulled the activation commit, its writes are judged by the preactivation catalog. A foreign parent written there is refused, and a Root write is refused.
- Two machines can make graph edits that are each valid alone but conflict when merged (a cycle, or a parent changed on both sides). Git merges the files, and nothing re-runs write validation on the merged result.
  - `tk sync` warns when a merge brought in unreadable files or invalid parent relationships (`sync: merged store has …`), and `tk audit` lists them.
  - A dependency cycle closed by two merged edits is caught by neither. The cycle check runs only on a write.
  - These conflicts are repaired by hand. The store does not refuse them retroactively.
- A store that is unreadable on one machine because of a conflict or a partial clone reports `complete: false` there. It does not report done.

## Release record

Fill one in per activation and keep it with the activation evidence (for example as a note on the rollout ticket).

```
Store:                    <central_root>   remote: <url>
Backup:                   <bundle path>    HEAD <sha>   verified by <who>, <when>
Audit before:             <path>           blockers resolved: <list or none>
Minimum tk:               vX.Y.Z (carries tk activate)

Machines and users:
  <host>/<user>   tk <ver>   loom <ver> (ticket <ver>)   shuttle <ver> (ticket <ver>)
                  weft <build> (spawns <tk path>)   warp <sha> rendered <when>
                  tk serve pids <pid: started>   tk watch <pid: started>
                  sync loop <what: restarted>

Restarts verified after install: <yes, ps lstart evidence>
Dry run:                  <path to activate-plan.json>   catalogued kinds reviewed by <who>
Activation commit:        <sha>   files: catalog.yaml, .namespace x<N>, no .md
Propagation:              <host>: already activated, inventory complete   (one line per host)
Audit after:              <path>   new findings: <none | list>
Walkthrough:              <each checklist item: pass/fail, evidence>
Adoption list:            <link>   applied commit <sha>   reviewed by <who>
Rollback path chosen if needed: retain compatible readers | reverse migration
```
