//go:build mage
// +build mage

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/fatih/color"
	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

var (
	tools = []string{
		"github.com/bufbuild/buf/cmd/buf",
		"github.com/bufbuild/buf/cmd/protoc-gen-buf-breaking",
		"github.com/bufbuild/buf/cmd/protoc-gen-buf-lint",
		"github.com/golangci/golangci-lint/cmd/golangci-lint",
		"github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway",
		"github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2",
		"golang.org/x/tools/cmd/cover",
		"golang.org/x/tools/cmd/goimports",
		"google.golang.org/grpc/cmd/protoc-gen-go-grpc",
		"google.golang.org/protobuf/cmd/protoc-gen-go",
		"../internal/cmd/protoc-gen-go-flipt-sdk/...",
	}

	Default = Build
)

func Bench() error {
	fmt.Println("Running benchmarks...")

	if err := sh.RunV("go", "test", "-run", "XXX", "-bench", ".", "-benchmem", "-short", "./..."); err != nil {
		return err
	}

	fmt.Println("Done.")
	return nil
}

// Bootstrap installs tools required for development
func Bootstrap() error {
	fmt.Println("Bootstrapping tools...")
	if err := os.MkdirAll("_tools", 0755); err != nil {
		return fmt.Errorf("creating dir: %w", err)
	}

	// create module if go.mod doesnt exist
	if _, err := os.Stat("_tools/go.mod"); os.IsNotExist(err) {
		cmd := exec.Command("go", "mod", "init", "tools")
		cmd.Dir = "_tools"
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return err
		}
	}

	install := []string{"install", "-v"}
	install = append(install, tools...)

	cmd := exec.Command("go", install...)
	cmd.Dir = "_tools"
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Build builds the project similar to a release build
func Build() error {
	mg.Deps(Clean)
	mg.Deps(UI)
	fmt.Println("Building...")

	if err := build(buildModeProd); err != nil {
		return err
	}

	fmt.Println("Done.")
	fmt.Printf("\nRun the following to start Flipt:\n")
	fmt.Printf("\n%v\n", color.CyanString(`./bin/flipt --config config/local.yml`))
	return nil
}

// Dev builds the project for development, without bundling assets
func Dev() error {
	mg.Deps(Clean)
	fmt.Println("Building...")

	if err := build(buildModeDev); err != nil {
		return err
	}

	fmt.Println("Done.")
	fmt.Printf("\nRun the following to start Flipt server:\n")
	fmt.Printf("\n%v\n", color.CyanString(`./bin/flipt --config config/local.yml`))
	fmt.Printf("\nIn another shell, run the following to start the UI in dev mode:\n")
	fmt.Printf("\n%v\n", color.CyanString(`cd ui && npm run dev`))
	return nil
}

type buildMode uint8

const (
	// buildModeDev builds the project for development, without bundling assets
	buildModeDev buildMode = iota
	// BuildModeProd builds the project similar to a release build
	buildModeProd
)

// UI installs UI dependencies and generates assets
func UI() error {
	fmt.Println("Installing UI deps...")

	// TODO: only install if package.json has changed
	cmd := exec.Command("npm", "ci")
	cmd.Dir = "ui"
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("installing UI deps: %w", err)
	}

	fmt.Println("Generating assets...")

	cmd = exec.Command("npm", "run", "build")
	cmd.Dir = "ui"
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func build(mode buildMode) error {
	buildDate := time.Now().UTC().Format(time.RFC3339)
	buildArgs := make([]string, 0)

	switch mode {
	case buildModeProd:
		buildArgs = append(buildArgs, "-tags", "assets")
	}

	gitCommit, err := sh.Output("git", "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("getting git commit: %w", err)
	}

	buildArgs = append([]string{"build", "-trimpath", "-ldflags", fmt.Sprintf("-X main.commit=%s -X main.date=%s", gitCommit, buildDate)}, buildArgs...)
	buildArgs = append(buildArgs, "-o", "./bin/flipt", "./cmd/flipt/")

	return sh.RunV("go", buildArgs...)
}

// Clean cleans up built files
func Clean() error {
	fmt.Println("Cleaning...")

	if err := sh.RunV("go", "mod", "tidy"); err != nil {
		return fmt.Errorf("tidying go.mod: %w", err)
	}

	clean := []string{"dist/*", "pkg/*", "bin/*", "ui/dist"}
	for _, dir := range clean {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("removing dir %q: %w", dir, err)
		}
	}

	return nil
}

// Prep prepares the project for building
func Prep() error {
	fmt.Println("Preparing...")
	mg.Deps(Clean)
	mg.Deps(UI)
	return nil
}

// Cover runs the tests and generates a coverage report
func Cover() error {
	mg.Deps(Test)
	fmt.Println("Running coverage...")
	return sh.RunV("go", "tool", "cover", "-html=coverage.txt")
}

var ignoreFmt = []string{"rpc/", "sdk/"}

// Fmt formats code
func Fmt() error {
	fmt.Println("Formatting...")
	files, err := findFilesRecursive(func(path string, _ os.FileInfo) bool {
		// only go files, ignoring generated files
		for _, dir := range ignoreFmt {
			if filepath.HasPrefix(path, dir) {
				return false
			}
		}
		return filepath.Ext(path) == ".go"
	})
	if err != nil {
		return fmt.Errorf("finding files: %w", err)
	}

	args := append([]string{"-w"}, files...)
	return sh.RunV("goimports", args...)
}

// Lint runs the linters
func Lint() error {
	fmt.Println("Linting...")

	if err := sh.RunV("golangci-lint", "run"); err != nil {
		return fmt.Errorf("linting: %w", err)
	}

	return sh.RunV("buf", "lint")
}

// Proto generates protobuf files and gRPC stubs
func Proto() error {
	mg.Deps(Bootstrap)
	fmt.Println("Generating proto files...")
	return sh.RunV("buf", "generate")
}

// Test runs the tests
func Test() error {
	fmt.Println("Testing...")

	env := map[string]string{
		"FLIPT_TEST_DATABASE_PROTOCOL": "sqlite3",
	}

	if os.Getenv("FLIPT_TEST_DATABASE_PROTOCOL") != "" {
		env["FLIPT_TEST_DATABASE_PROTOCOL"] = os.Getenv("FLIPT_TEST_DATABASE_PROTOCOL")
	}

	return sh.RunWithV(env, "go", "test", "-v", "-covermode=atomic", "-count=1", "-coverprofile=coverage.txt", "-timeout=60s", "./...")
}

// findFilesRecursive recursively traverses from the CWD and invokes the given
// match function on each regular file to determine if the given path should be
// returned as a match. It ignores files in .git directories.
func findFilesRecursive(match func(path string, info os.FileInfo) bool) ([]string, error) {
	var matches []string
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Don't look for files in git directories
		if info.Mode().IsDir() && filepath.Base(path) == ".git" {
			return filepath.SkipDir
		}

		if !info.Mode().IsRegular() {
			// continue
			return nil
		}

		if match(filepath.ToSlash(path), info) {
			matches = append(matches, path)
		}
		return nil
	})
	return matches, err
}
