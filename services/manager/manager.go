package servicesmanager

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"runtime"
	"strings"

	"github.com/google/go-github/v50/github"
	"github.com/vertex-center/vertex-core-golang/console"
	"github.com/vertex-center/vertex/services"
	"github.com/vertex-center/vertex/services/runners"
)

var logger = console.New("vertex::services-manager")

func ListInstalled() (map[string]*services.InstalledService, error) {
	entries, err := os.ReadDir("servers")
	if err != nil {
		return nil, err
	}

	var allServices = map[string]*services.InstalledService{}

	for _, entry := range entries {
		if entry.IsDir() {
			data, err := os.ReadFile(path.Join("servers", entry.Name(), ".vertex", "service.json"))
			if err != nil {
				logger.Warn(fmt.Sprintf("service %s has no '.vertex/service.json' file", entry.Name()))
				continue
			}

			var service services.Service
			err = json.Unmarshal(data, &service)
			if err != nil {
				return nil, err
			}

			allServices[service.ID] = &services.InstalledService{
				Service: service,
				Status:  "off",
			}
		}
	}

	for key, service := range allServices {
		runner, err := runners.GetRunner(key)
		if err != nil {
			continue
		}
		service.Status = runner.Status()
	}

	return allServices, nil
}

func Download(s services.Service) error {
	if strings.HasPrefix(s.Repository, "github") {
		client := github.NewClient(nil)

		split := strings.Split(s.Repository, "/")

		owner := split[1]
		repo := split[2]

		release, _, err := client.Repositories.GetLatestRelease(context.Background(), owner, repo)
		if err != nil {
			return errors.New(fmt.Sprintf("failed to retrieve the latest github release for %s", s.Repository))
		}

		platform := fmt.Sprintf("%s_%s", runtime.GOOS, runtime.GOARCH)

		for _, asset := range release.Assets {
			if strings.Contains(*asset.Name, platform) {
				basePath := path.Join("servers", s.ID)
				archivePath := path.Join(basePath, fmt.Sprintf("%s.tar.gz", s.ID))

				err := downloadFile(*asset.BrowserDownloadURL, basePath, archivePath)
				if err != nil {
					return err
				}

				err = untarFile(basePath, archivePath)
				if err != nil {
					return err
				}

				err = os.Remove(archivePath)
				if err != nil {
					return err
				}

				break
			}
		}
	}

	return nil
}

func downloadFile(url string, basePath string, archivePath string) error {
	err := os.Mkdir(basePath, os.ModePerm)
	if err != nil {
		return err
	}

	res, err := http.Get(url)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	file, err := os.Create(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, res.Body)
	return err
}

func untarFile(basePath string, archivePath string) error {
	archive, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer archive.Close()

	stream, err := gzip.NewReader(archive)
	if err != nil {
		return err
	}
	defer stream.Close()

	reader := tar.NewReader(stream)

	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			return err
		}

		filepath := path.Join(basePath, header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			err = os.MkdirAll(filepath, os.ModePerm)
			if err != nil {
				return err
			}
		case tar.TypeReg:
			err := os.MkdirAll(path.Dir(filepath), os.ModePerm)
			if err != nil {
				return err
			}

			file, err := os.Create(filepath)
			if err != nil {
				return err
			}

			_, err = io.Copy(file, reader)
			if err != nil {
				return err
			}

			err = os.Chmod(filepath, 0755)
			if err != nil {
				return err
			}

			file.Close()
		default:
			return errors.New(fmt.Sprintf("unknown flag type (%s) for file '%s'", header.Typeflag, header.Name))
		}
	}

	return nil
}