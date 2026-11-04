package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"strings"
	"time"
)

type RunnerApplication struct {
	OS       string `json:"os"`
	Arch     string `json:"architecture"`
	Filename string `json:"filename"`
	URL      string `json:"download_url"`
	Hash     string `json:"sha256_checksum"`
}

func runnerApplication(api API, repo string) (RunnerApplication, error) {
	var applications []RunnerApplication
	path := "repos/" + repo + "/actions/runners/downloads"
	if err := api.Call("GET", path, nil, &applications); err != nil {
		return RunnerApplication{}, err
	}
	osName := map[string]string{"windows": "win", "darwin": "osx", "linux": "linux"}
	arch := map[string]string{"amd64": "x64", "arm64": "arm64", "arm": "arm"}
	var found []RunnerApplication
	for _, app := range applications {
		if app.OS == osName[runtime.GOOS] && app.Arch == arch[runtime.GOARCH] {
			found = append(found, app)
		}
	}
	if len(found) != 1 || !digestPattern.MatchString(found[0].Hash) {
		return RunnerApplication{}, errors.New("OFFICIAL_RUNNER_SOURCE_UNAVAILABLE")
	}
	app := found[0]
	filename := regexp.MustCompile(
		`^actions-runner-(win|osx|linux)-(x64|arm64|arm)-\d+\.\d+\.\d+\.(zip|tar\.gz)$`)
	u, err := url.Parse(app.URL)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" ||
		!strings.HasPrefix(u.Path, "/actions/runner/releases/download/v") ||
		!strings.HasSuffix(u.Path, "/"+app.Filename) ||
		!strings.HasPrefix(app.Filename, "actions-runner-"+app.OS+"-"+app.Arch+"-") ||
		!filename.MatchString(app.Filename) {
		return RunnerApplication{}, errors.New("UNTRUSTED_RUNNER_DOWNLOAD")
	}
	return app, nil
}

type RunnerDownload func(RunnerApplication, string) error

func OfficialRunnerDownload(app RunnerApplication, destination string) error {
	client := http.Client{Timeout: 5 * time.Minute,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			host := request.URL.Hostname()
			if len(via) > 5 || request.URL.Scheme != "https" || request.URL.User != nil ||
				(host != "github.com" && host != "release-assets.githubusercontent.com" &&
					host != "objects.githubusercontent.com") {
				return errors.New("UNTRUSTED_RUNNER_REDIRECT")
			}
			return nil
		}}
	response, err := client.Get(app.URL)
	if err != nil {
		return errors.New("RUNNER_DOWNLOAD_FAILED")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("RUNNER_DOWNLOAD_DENIED")
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("RUNNER_ARCHIVE_FILE_UNAVAILABLE")
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash),
		io.LimitReader(response.Body, 512*1024*1024+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || written > 512*1024*1024 ||
		hex.EncodeToString(hash.Sum(nil)) != app.Hash {
		return errors.New("RUNNER_DOWNLOAD_HASH_MISMATCH")
	}
	return nil
}
