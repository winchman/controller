package main

import (
	"../"
	"log"
)

const testConfig = `
blocks:
  - name: block-A
    disable_cache: true
    dockerfile: Dockerfile.first
  - name: block-B
    push_image: true
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
		Registry:    "quay.io/winchman",
	})

	if err != nil {
		log.Printf("%s", err)
	}
}
