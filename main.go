package main

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Luzifer/go_helpers/str"
	"github.com/Luzifer/rconfig"
)

const (
	STATUS_DIVERGED = "diverged"
	STATUS_AHEAD    = "ahead"
	STATUS_BEHIND   = "behind"
	STATUS_UPTODATE = "uptodate"

	MOD_UNKNOWN  = "unknown"
	MOD_ADDED    = "added"
	MOD_MODIFIED = "modified"
	MOD_REMOVED  = "removed"
	MOD_DELETED  = "deleted"
	MOD_STASHED  = "stashed"
	MOD_CHANGED  = "changed" // Special status to filter all repos having any changes

	FILTER_REMOTE = "remote"
)

var (
	cfg = struct {
		Filter         []string `flag:"filter,f" default:"" description:"Attributes to filter for"`
		Format         string   `flag:"format" vardefault:"format" description:"Output format"`
		Or             bool     `flag:"or" default:"false" description:"Switch combining of filters from AND to OR"`
		Search         string   `flag:"search,s" default:"" description:"String to search for in output"`
		VersionAndExit bool     `flag:"version" default:"false" description:"Prints current version and exits"`
	}{}

	porcelainFlags = map[string]string{
		"??": MOD_UNKNOWN,
		"A ": MOD_ADDED,
		"M ": MOD_ADDED,
		" M": MOD_MODIFIED,
		"AM": MOD_MODIFIED,
		" T": MOD_MODIFIED,
		"R ": MOD_REMOVED,
		" D": MOD_DELETED,
		"D ": MOD_DELETED,
		"AD": MOD_DELETED,
	}

	flagSigns = map[string]string{
		MOD_UNKNOWN:  "U",
		MOD_ADDED:    "A",
		MOD_MODIFIED: "M",
		MOD_REMOVED:  "R",
		MOD_DELETED:  "D",
		MOD_STASHED:  "S",
	}

	statusSigns = map[string]string{
		STATUS_DIVERGED: "↔",
		STATUS_AHEAD:    "→",
		STATUS_BEHIND:   "←",
		STATUS_UPTODATE: "=",
	}

	collectionStatus        = []string{STATUS_AHEAD, STATUS_BEHIND, STATUS_DIVERGED, STATUS_UPTODATE}
	collectionModifications = []string{MOD_ADDED, MOD_UNKNOWN, MOD_REMOVED, MOD_STASHED, MOD_DELETED, MOD_MODIFIED, MOD_CHANGED}

	traverseResults = make(chan string, 10)

	version = "dev"
)

type repoStatus struct {
	Modifications map[string]bool
	Branch        string
	Remote        string
	RemoteStatus  string

	Path string
}

func getRepoStatus(path string) (*repoStatus, error) {
	r := &repoStatus{
		Modifications: map[string]bool{},
		Path:          path,
	}

	if err := r.getCurrentBranch(); err != nil {
		return nil, err
	}

	if err := r.getRemote(); err != nil {
		return nil, err
	}

	if err := r.getModifications(); err != nil {
		return nil, err
	}

	return r, nil
}

func (r repoStatus) matches(filters []string) bool {
	match := !cfg.Or

	for _, f := range filters {
		if len(strings.TrimSpace(f)) == 0 {
			continue
		}

		expect := !strings.HasPrefix(f, "no-")
		f = strings.TrimPrefix(f, "no-")

		if str.StringInSlice(f, collectionStatus) {
			match = andOrAdd(match, cfg.Or, (r.RemoteStatus == f) == expect)
		}

		if str.StringInSlice(f, collectionModifications) {
			match = andOrAdd(match, cfg.Or, r.Modifications[f] == expect)
		}

		switch f {
		case FILTER_REMOTE:
			match = andOrAdd(match, cfg.Or, (r.Remote != "") == expect)
		}
	}

	return match
}

func andOrAdd(in, or, add bool) bool {
	if or {
		return in || add
	}
	return in && add
}

func (r repoStatus) String() string {
	tpl, err := template.New("output").Parse(cfg.Format)
	if err != nil {
		log.Fatalf("Cannot parse format string: %s", err)
	}

	buf := bytes.NewBuffer([]byte{})
	values := map[string]interface{}{
		"State":  statusSigns[r.RemoteStatus],
		"Path":   r.Path,
		"Remote": r.Remote,
		"Branch": r.Branch,
	}

	for key, char := range flagSigns {
		if r.Modifications[key] {
			values[char] = char
		} else {
			values[char] = " "
		}
	}

	if err := tpl.Execute(buf, values); err != nil {
		log.Fatalf("Unable to execute template: %s", err)
	}
	return buf.String()
}

func init() {
	rconfig.SetVariableDefaults(map[string]string{
		"format": `[{{.U}}{{.A}}{{.M}}{{.R}}{{.D}}{{.S}} {{.State}}] {{.Path}} ({{if .Remote}}{{.Remote}} » {{end}}{{.Branch}})`,
	})

	if err := rconfig.Parse(&cfg); err != nil {
		log.Fatalf("Unable to parse commandline options: %s", err)
	}

	if cfg.VersionAndExit {
		fmt.Printf("git-changerelease %s\n", version)
		os.Exit(0)
	}
}

func main() {
	p := "."
	if len(rconfig.Args()) == 2 {
		p = rconfig.Args()[1]
	}

	wg := sync.WaitGroup{}
	go func() {
		for dir := range traverseResults {
			wg.Add(1)
			rs, err := getRepoStatus(dir)
			if err != nil {
				log.Fatalf("Error reading repo status of %q: %s", dir, err)
			}
			if rs.matches(cfg.Filter) && strings.Contains(rs.String(), cfg.Search) {
				fmt.Printf("%s\n", rs)
			}
			wg.Done()
		}
	}()

	if err := filepath.Walk(p, walkerFkt); err != nil {
		log.Fatalf("An error happened while traversing paths: %s", err)
	}

	for len(traverseResults) > 0 {
		<-time.After(time.Millisecond)
	}

	wg.Wait()
}

func walkerFkt(path string, info os.FileInfo, err error) error {
	if err != nil {
		return err
	}

	if !info.IsDir() {
		return nil
	}

	if strings.HasSuffix(path, ".git") {
		traverseResults <- filepath.Dir(path)
	}

	return nil
}

func (r *repoStatus) getModifications() error {
	buf, err := execGitCommand(r.Path, false, "status", "--porcelain", "-b")
	if err != nil {
		return err
	}

	for _, line := range strings.Split(buf.String(), "\n") {
		if len(line) < 3 {
			continue
		}

		switch line[0:2] {
		case "##":
			if strings.Contains(line, "ahead") && strings.Contains(line, "behind") {
				r.RemoteStatus = STATUS_DIVERGED
			} else if strings.Contains(line, "ahead") {
				r.RemoteStatus = STATUS_AHEAD
			} else if strings.Contains(line, "behind") {
				r.RemoteStatus = STATUS_BEHIND
			} else {
				r.RemoteStatus = STATUS_UPTODATE
			}
		default:
			if flag, ok := porcelainFlags[line[0:2]]; ok {
				r.Modifications[flag] = true
				r.Modifications[MOD_CHANGED] = true
			}
		}
	}

	if _, err = execGitCommand(r.Path, false, "rev-parse", "--verify", "refs/stash"); err == nil {
		r.Modifications[MOD_STASHED] = true
		r.Modifications[MOD_CHANGED] = true
	}

	return nil
}

func (r *repoStatus) getRemote() error {
	buf, err := execGitCommand(r.Path, false, "remote", "-v")
	if err != nil {
		return err
	}

	if len(strings.TrimSpace(buf.String())) == 0 {
		return nil
	}

	rex := regexp.MustCompile(`^origin\s+([^ ]+) \(push\)$`)
	for _, line := range strings.Split(buf.String(), "\n") {
		if matches := rex.FindStringSubmatch(line); len(matches) > 1 {
			r.Remote = matches[1]
			return nil
		}
	}

	return nil
}

func (r *repoStatus) getCurrentBranch() error {
	buf, err := execGitCommand(r.Path, false, "symbolic-ref", "--quiet", "HEAD")
	if err != nil {
		buf, err = execGitCommand(r.Path, false, "rev-parse", "--short", "HEAD")
	}
	r.Branch = strings.TrimPrefix(strings.TrimSpace(buf.String()), "refs/heads/")
	return err
}

func execGitCommand(path string, enableStderr bool, args ...string) (*bytes.Buffer, error) {
	buf := bytes.NewBuffer([]byte{})

	cmd := exec.Command("git", args...)
	cmd.Dir = path
	cmd.Stdout = buf
	if enableStderr {
		cmd.Stderr = os.Stderr
	}

	return buf, cmd.Run()
}
