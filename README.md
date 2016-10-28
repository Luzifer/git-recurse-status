# Luzifer / git-recurse-status

Wrapper around git to display status of a whole tree of directories to detect uncommitted or not pushed changes.

## Features

- Recurse through a directory tree
- Display changes in a short manner
- Filter output by full-text search
- Filter output by a combinable set of filters
- Change format of output using Go templates

## Usage

```bash
# git-recurse-status --help
Usage of git-recurse-status:
  -f, --filter stringSlice   Attributes to filter for
      --format string        Output format (default "[{{.U}}{{.A}}{{.M}}{{.R}}{{.D}}{{.S}} {{.State}}] {{.Path}} ({{if .Remote}}{{.Remote}} » {{end}}{{.Branch}})")
      --or                   Switch combining of filters from AND to OR
  -s, --search string        String to search for in output
      --version              Prints current version and exits
```

### Filters

Possible filters:

- By status against the remote:
  - `diverged` - Local and remote do have different commits
  - `ahead` - Local has new commits
  - `behind` - Remote has new commits (remote repository is not automatically fetched!)
  - `uptodate` - No known changes against remote
- By local changes:
  - `untracked` - There are files not yet known to git
  - `added` - Local files are added to the index but not yet committed
  - `modified` - Local files are modified but not added to the index
  - `removed` - Local files were removed but not yet deleted from the index
  - `deleted` - Files are marked to be removed in the index
  - `stashed` - You have changes in the stash
  - `changed` - Shortcut for all local changes at once
- Other filters
  - `remote` - Repositories having a remote named "origin" set

All filters mentioned above are extendable with the prefix `no-` to negate them (example: `no-changes`).

Example usage for filters:

```bash
# git recurse-status -f no-uptodate
[       →] dockerproxy_config (git@bitbucket.org:luzifer/dockerproxy_config.git » master)
[  M    →] knut-ws (git@github.com:Luzifer/workstation.git » master)

# git recurse-status -f no-uptodate -f modified
[  M    →] knut-ws (git@github.com:Luzifer/workstation.git » master)
```
