package buildcontroller

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"path"

	"github.com/pivotal-golang/archiver/extractor"
)

// BuildPackageOptions are options for download a build package.
type BuildPackageOptions struct {
	URL          string
	SubDirectory string
	// TODO: add git repo options here
}

// CreateBuildPackageDirectory creates a directory on disk containing a build package for use
// by 'docker build'.
func CreateBuildPackageDirectory(buildPackage BuildPackageOptions) (string, error) {
	// Load the build package from the URL.
	log.Printf("Preparing build package: %s", buildPackage.URL)
	resp, err := http.Get(buildPackage.URL)
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()

	// Find the MIME type of the build package and use it to untar/unzip, if necessary.
	mimeType := resp.Header["Content-Type"][0]
	if mimeType == "" {
		mimeType = "text/plain"
	}

	// Extract the build package into a reader stream.
	directory, err := extractBuildPackage(resp.Body, mimeType, buildPackage.SubDirectory)
	if err != nil {
		return "", err
	}

	return directory, nil
}

func extractFromArchive(body io.ReadCloser, helper extractor.Extractor, subDirectory string) (string, error) {
	// Download the build archive to a temporary file.
	archiveFile, err := ioutil.TempFile("", "build_archive")
	if err != nil {
		return "", err
	}

	log.Printf("Downloading build package archive to %s", archiveFile.Name())
	_, err = io.Copy(archiveFile, body)
	if err != nil {
		return "", err
	}

	archiveFile.Close()
	body.Close()

	// Create a temporary directory for the build pack.
	tempDirectory, err := ioutil.TempDir("", "build_pack")
	if err != nil {
		return "", err
	}

	// Extract the contents of the archive into the temporary directory.
	log.Printf("Extracting build package archive to directory %s", tempDirectory)
	err = helper.Extract(archiveFile.Name(), tempDirectory)
	if err != nil {
		return "", err
	}

	rootLocation := tempDirectory
	if subDirectory != "" {
		rootLocation = path.Join(rootLocation, subDirectory)
	}

	return rootLocation, nil
}

func extractBuildPackage(body io.ReadCloser, mimeType string, subDirectory string) (string, error) {
	log.Printf("Found build package of type %s with sub directory '%s'", mimeType, subDirectory)

	switch mimeType {
	case "application/zip", "application/x-zip-compressed":
		return extractFromArchive(body, extractor.NewZip(), subDirectory)

	case "application/x-tar", "application/gzip", "application/x-gzip":
		return extractFromArchive(body, extractor.NewTgz(), subDirectory)
	}

	return "", fmt.Errorf("Unsupported kind of build package")
}
