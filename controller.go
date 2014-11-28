package buildcontroller

import (
	"errors"
	"github.com/sylphon/builder-core"
	"github.com/sylphon/builder-core/unit-config"
	"github.com/sylphon/graph-builder"
	"log"
)

type buildResultStruct struct {
	job      *buildgraph.Job
	jobError error
}

// InvokeBuild starts a full build, with the jobs described in the yamlConfig and the build
// context described by the given options.
func InvokeBuild(yamlConfig string, buildPackage BuildPackageOptions) error {
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
	buildPackageDirectory, err := CreateBuildPackageDirectory(buildPackage)
	if err != nil {
		return err
	}

	// Start the initial build units.
	return invokeBuild(graph, initialJobs, buildPackageDirectory)
}

func buildUnitConfig(job *buildgraph.Job) *unitconfig.UnitConfig {
	return &unitconfig.UnitConfig{
		Version: 1,
		ContainerArr: []*unitconfig.ContainerSection{
			&unitconfig.ContainerSection{
				Name:       job.Name,
				Dockerfile: job.Dockerfile,
				Registry:   "quay.io/rafecolton", // TODO: read this from the config.
				Project:    job.ImageName,        // TODO: Generate this if not specified (but only for skippush true)
				Tags:       job.Tags,
				SkipPush:   job.SkipPush,
			},
		},
	}
}

func invokeBuild(graph *buildgraph.Graph, initialJobs []*buildgraph.Job, buildPackageDirectory string) error {
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
			unitConfig := buildUnitConfig(job)

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
