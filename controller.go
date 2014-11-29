package buildcontroller

import (
	"errors"
	"fmt"
	"github.com/sylphon/builder-core"
	"github.com/sylphon/builder-core/unit-config"
	"github.com/sylphon/graph-builder"
	"log"
)

// PushCredentials for specifying the default credentials when pushing images.
type PushCredentials struct {
	Username string
	Password string
}

// InvokeBuildOptions are options for invoking a build in the controller.
type InvokeBuildOptions struct {
	Registry               string
	ProjectName            string
	BuildPackage           BuildPackageOptions
	DefaultPushCredentials PushCredentials
}

type buildResultStruct struct {
	job      *buildgraph.Job
	jobError error
}

// InvokeBuild starts a full build, with the jobs described in the yamlConfig and the build
// context described by the given options.
func InvokeBuild(yamlConfig string, options InvokeBuildOptions) error {
	// Parse a graph from the YAML build config.
	graph, err := buildgraph.ParseGraphFromYAML([]byte(yamlConfig))
	if err != nil {
		return err
	}

	// Find the initial set of units to build.
	initialJobs, err := graph.GetDependants(nil, nil)
	if err != nil {
		return err
	}

	if len(initialJobs) == 0 {
		return errors.New("No independent build units defined")
	}

	// Load the build package.
	buildPackageDirectory, err := CreateBuildPackageDirectory(options.BuildPackage)
	if err != nil {
		return err
	}

	// Start the initial build units.
	return invokeBuild(graph, initialJobs, buildPackageDirectory, options)
}

func buildUnitConfig(job *buildgraph.Job, options InvokeBuildOptions) (*unitconfig.UnitConfig, error) {
	// Determine a project name for the unit. The project name is determined by one of three
	// settings:
	//  - The job's push info image name
	//  - The job's image name
	//  - The overall project name (but only if this is a skip-push step; otherwise, we want it to
	//    be explicit)
	projectName := job.PushInfo.Image
	if projectName == "" {
		projectName = job.ImageName
	}

	if projectName == "" {
		if job.SkipPush {
			projectName = options.ProjectName
		} else {
			return nil, fmt.Errorf("Missing ImageName for non-SkipPush unit %s", job.Name)
		}
	}

	// Read the credentials for the push.
	username := options.DefaultPushCredentials.Username
	password := options.DefaultPushCredentials.Password

	if job.PushInfo.Credentials.Username != "" && job.PushInfo.Credentials.Password != "" {
		username = job.PushInfo.Credentials.Username
		password = job.PushInfo.Credentials.Password
	}

	return &unitconfig.UnitConfig{
		Version: 1,
		ContainerArr: []*unitconfig.ContainerSection{
			&unitconfig.ContainerSection{
				Name:       job.Name,
				Dockerfile: job.Dockerfile,
				Registry:   options.Registry,
				Project:    projectName,
				Tags:       job.Tags,
				SkipPush:   job.SkipPush,
				CfgUn:      username,
				CfgPass:    password,
			},
		},
	}, nil
}

func invokeBuild(graph *buildgraph.Graph, initialJobs []*buildgraph.Job, buildPackageDirectory string, options InvokeBuildOptions) error {
	currentJobs := initialJobs
	var finishedJobs []*buildgraph.Job
	var brokenJobs []*buildgraph.Job

	for {
		// Create a channel to collect the done state of each of the jobs to run.
		done := make(chan buildResultStruct, len(currentJobs))

		// Start each of the build units in their own gorountine. This allows the units to run
		// concurrently.
		log.Printf("Jobs to run: %d", len(currentJobs))
		for _, job := range currentJobs {
			unitConfig, err := buildUnitConfig(job, options)
			if err != nil {
				return err
			}

			go func() {
				log.Printf("Starting Job: %s", job.Name)
				jobError := runner.RunBuildSynchronously(unitConfig, buildPackageDirectory)
				done <- buildResultStruct{
					job:      job,
					jobError: jobError,
				}
			}()
		}

		// Wait for each of the jobs to complete. As each job completes, add it to the finished
		// or broken sets, and append any artifacts created.
		for i := 0; i < len(currentJobs); i++ {
			result := <-done
			if result.jobError == nil {
				finishedJobs = append(finishedJobs, result.job)
			} else {
				brokenJobs = append(brokenJobs, result.job)
				log.Printf("Block %s failed with error: %s", result.job.Name, result.jobError)
			}
		}

		// Retrieve the next set of jobs to perform.
		nextJobs, err := graph.GetDependants(finishedJobs, brokenJobs)
		if err != nil {
			return err
		}

		if len(nextJobs) == 0 {
			break
		}

		currentJobs = nextJobs
	}

	if len(brokenJobs) > 0 {
		return errors.New("Early termination due to one or more build units failing")
	}

	return nil
}
