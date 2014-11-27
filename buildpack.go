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

type BuildPackageOptions struct {
	Url          string
	SubDirectory string
	// TODO: add git repo options here
}

func CreateBuildPackageDirectory(build_package BuildPackageOptions) (string, error) {
	// Load the build package from the URL.
	log.Printf("Preparing build package: %s", build_package.Url)
	resp, err := http.Get(build_package.Url)
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()

	// Find the MIME type of the build package and use it to untar/unzip, if necessary.
	mime_type := resp.Header["Content-Type"][0]
	if mime_type == "" {
		mime_type = "text/plain"
	}

	// Extract the build package into a reader stream.
	directory, err := extractBuildPackage(resp.Body, mime_type, build_package.SubDirectory)
	if err != nil {
		return "", err
	}

	return directory, nil
}

func extractFromArchive(body io.ReadCloser, helper extractor.Extractor, sub_directory string) (string, error) {
	// Download the build archive to a temporary file.
	archive_file, err := ioutil.TempFile("", "build_archive")
	if err != nil {
		return "", err
	}

	log.Printf("Downloading build package archive to %s", archive_file.Name())
	_, err = io.Copy(archive_file, body)
	if err != nil {
		return "", err
	}

	archive_file.Close()
	body.Close()

	// Create a temporary directory for the build pack.
	temp_directory, err := ioutil.TempDir("", "build_pack")
	if err != nil {
		return "", err
	}

	// Extract the contents of the archive into the temporary directory.
	log.Printf("Extracting build package archive to directory %s", temp_directory)
	err = helper.Extract(archive_file.Name(), temp_directory)
	if err != nil {
		return "", err
	}

	root_location := temp_directory
	if sub_directory != "" {
		root_location = path.Join(root_location, sub_directory)
	}

	return root_location, nil
}

func extractBuildPackage(body io.ReadCloser, mime_type string, sub_directory string) (string, error) {
	log.Printf("Found build package of type %s with sub directory '%s'", mime_type, sub_directory)

	switch mime_type {
	case "application/zip", "application/x-zip-compressed":
		return extractFromArchive(body, extractor.NewZip(), sub_directory)

	case "application/x-tar", "application/gzip", "application/x-gzip":
		return extractFromArchive(body, extractor.NewTgz(), sub_directory)
	}

	return "", fmt.Errorf("Unsupported kind of build package")
}
