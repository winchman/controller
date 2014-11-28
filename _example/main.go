package main

import (
	"../"
	"log"
)

const testConfig = `
blocks:
  - name: block-A
    skip_push: true
    disable_cache: true
    dockerfile: Dockerfile.first
    image_name: first
  - name: block-B
    image_name: second
    requires:
      - block-A
    disable_cache: true
    dockerfile: Dockerfile.second
    tags:
      - latest
    push_info:
      image: quay.io/namespace/repo:latest
      credentials:
        username: fakeuser
        password: fakepass
`

func main() {
	options := buildcontroller.BuildPackageOptions{}
	options.URL = "https://github.com/josephschorr/testmultibuild/archive/master.zip"
	options.SubDirectory = "testmultibuild-master"

	err := buildcontroller.InvokeBuild(testConfig, options)

	if err != nil {
		log.Printf("%s", err)
	}
}
