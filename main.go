package main

import (
	goflag "flag"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/go-logr/logr"
	flag "github.com/spf13/pflag"
	"k8s.io/klog"
	"k8s.io/klog/klogr"
)

// multiversion is a tool that builds a Hugo content/ directory based on
// documents contained in different branches of a single repository.
// It may also provide additional utilities to make working with
// multi-version Hugo sites easier in future.

var (
	repoURL string
	repoContentDir string
	outputDir string
	latestBranch string
	branches []string
	debug bool

	log logr.Logger
)

func init() {
	flag.StringVar(&repoURL, "repo-url", "", "Git repository URL of the repository containing a content/ directory")
	flag.StringVar(&repoContentDir, "repo-content-dir", "content", "Path to the 'content' directory in the source git repository. This must be the same on all branches.")
	flag.StringVar(&outputDir, "output-dir", "content", "output content/ directory")
	flag.StringVar(&latestBranch, "latest-branch", "", "If true, the 'latest' version will also be fetched ")
	flag.StringSliceVar(&branches, "branches", []string{}, "version=branch pairs that should be included in the generated content/ directory")
	flag.BoolVar(&debug, "debug", false, "if true, do not clean up the temporary directory used for building the output")
}

func main() {
	klog.InitFlags(goflag.CommandLine)
	if err := goflag.CommandLine.Lookup("logtostderr").Value.Set("true"); err != nil {
		os.Exit(2)
	}

	// add just the --v flag to the pflag flagset
	flag.CommandLine.AddGoFlag(goflag.CommandLine.Lookup("v"))
	flag.Parse()

	log = klogr.New()
	if !validateFlags() {
		os.Exit(1)
	}
	if err := run(); err != nil {
		log.Error(err, "Failed to run")
		os.Exit(1)
	}
}

func validateFlags() bool {
	valid := true
	valid = notEmpty("repo-url", repoURL) && valid
	valid = notEmpty("repo-content-dir", repoContentDir) && valid
	valid = notEmpty("output-dir", outputDir) && valid
	return valid
}

func notEmpty(name, val string) bool {
	if val == "" {
		log.Info("--"+name+" must be specified")
		return false
	}
	return true
}

// parseBranchesFlag converts a list of a=b mapping strings into a map.
// If one of the elements of 'branches' does not contain an = sign, the string
// value will be used as both the version name and branch name in the map.
func parseBranchesFlag(branches []string) map[string]string {
	out := make(map[string]string)
	for _, b := range branches {
		splitStr := strings.Split(b, "=")
		// no = sign, use the string as the version number and branch name
		if len(splitStr) == 1 {
			out[b] = b
			continue
		}
		out[splitStr[0]] = strings.Join(splitStr[1:], "")
	}
	return out
}

// fetchRepository will use the system installed git command to fetch a copy of
// the repository at the specified revision
func fetchRepository(log logr.Logger, tmpdir, repoURL, version, branchName string) (string, error) {
	log.Info("Fetching repository at revision")
	cloneDir := filepath.Join(tmpdir, "repo", version)
	if err := runCommand(log, "git", "clone", "-b", branchName, repoURL, cloneDir); err != nil {
		return "", err
	}
	return cloneDir, nil
}

func run() error {
	if latestBranch == "" && len(branches) == 0 {
		log.Info("Nothing to do!")
		return nil
	}

	tmpdir, err := ioutil.TempDir("", "hugo-multiversion-")
	if err != nil {
		return err
	}
	defer cleanup(log, tmpdir)

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		log.Info("Error creating output directory")
		return err
	}

	versionMap := parseBranchesFlag(branches)
	if latestBranch != "" {
		versionMap["latest"] = latestBranch
	}
	for vers, branch := range versionMap {
		log := log.WithValues("version", vers, "branch", branch)
		log.Info("Adding version to list to generate")

		loc, err := fetchRepository(log, tmpdir, repoURL, vers, branch)
		if err != nil {
			log.Error(err, "Failed to fetch repository")
			return err
		}

		log.Info("Fetched repository", "path", loc)
		log.Info("Copying content to output directory")

		src := filepath.Join(loc, repoContentDir)
		dst := filepath.Join(outputDir, vers)
		if err := copyDir(src, dst); err != nil {
			log.Error(err, "Failed to copy content from source repository to output directory")
			return err
		}
	}

	log.Info("Built content directory")
	return nil
}

// copyFile copies a single file from src to dst
func copyFile(src, dst string) error {
	var err error
	var srcfd *os.File
	var dstfd *os.File
	var srcinfo os.FileInfo

	if srcfd, err = os.Open(src); err != nil {
		return err
	}
	defer srcfd.Close()

	if dstfd, err = os.Create(dst); err != nil {
		return err
	}
	defer dstfd.Close()

	if _, err = io.Copy(dstfd, srcfd); err != nil {
		return err
	}
	if srcinfo, err = os.Stat(src); err != nil {
		return err
	}
	return os.Chmod(dst, srcinfo.Mode())
}

// copyDir copies a whole directory recursively
func copyDir(src string, dst string) error {
	var err error
	var fds []os.FileInfo
	var srcinfo os.FileInfo

	if srcinfo, err = os.Stat(src); err != nil {
		return err
	}

	if err = os.MkdirAll(dst, srcinfo.Mode()); err != nil {
		return err
	}

	if fds, err = ioutil.ReadDir(src); err != nil {
		return err
	}
	for _, fd := range fds {
		srcfp := path.Join(src, fd.Name())
		dstfp := path.Join(dst, fd.Name())

		if fd.IsDir() {
			if err = copyDir(srcfp, dstfp); err != nil {
				return err
			}
		} else {
			if err = copyFile(srcfp, dstfp); err != nil {
				return err
			}
		}
	}
	return nil
}

func cleanup(log logr.Logger, dir string) {
	log = log.WithValues("directory", dir)
	if debug {
		log.Info("Skipping cleaning up temporary directory")
		return
	}
	if err := os.RemoveAll(dir); err != nil {
		log.Error(err, "Failed to cleanup temporary directory")
		return
	}
	log.Info("Cleaned up temporary directory")
}

func runCommand(log logr.Logger, name string, args ...string) error {
	log = log.WithValues("cmd", name, "args", args)
	cmd := exec.Command(name, args...)
	if debug {
		log.Info("Running command")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	if err := cmd.Run(); err != nil {
		log.Error(err, "Error running command")
		return err
	}
	return nil
}
