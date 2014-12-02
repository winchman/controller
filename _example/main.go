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
  - name: block-B
    image_name: second
    requires:
      - block-A
    disable_cache: true
    dockerfile: Dockerfile.second
    tags:
      - latest
    push_info:
      image: repo
      credentials:
        username: fakeuser
        password: fakepass
`

func main() {
	err := buildcontroller.InvokeBuild(testConfig, buildcontroller.InvokeBuildOptions{
		BuildPackage: buildcontroller.BuildPackageOptions{
			URL:          "https://github.com/josephschorr/testmultibuild/archive/master.zip",
			SubDirectory: "testmultibuild-master",
		},
		ProjectName: "exampleproject",
		Registry:    "quay.io/sylphon",
	})

	if err != nil {
		log.Printf("%s", err)
	}
}
