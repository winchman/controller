package buildcontroller

import (
	"errors"
	"github.com/sylphon/builder-core"
	"github.com/sylphon/builder-core/unit-config"
	"github.com/sylphon/graph-builder"
	"log"
)

type BuildResultStruct struct {
	job       *buildgraph.Job
	job_error error
}

func InvokeBuild(yaml_config string, build_package BuildPackageOptions) error {
	// Parse a graph from the YAML build config.
	graph, err := buildgraph.ParseGraphFromYAML([]byte(yaml_config))
	if err != nil {
		return err
	}

	// Find the initial set of units to build.
	empty := make([]*buildgraph.Job, 0)
	initial_jobs, err := graph.GetDependants(empty, empty)
	if err != nil {
		return err
	}

	if len(initial_jobs) == 0 {
		return errors.New("No independent build units defined")
	}

	// Load the build package.
	build_package_directory, err := CreateBuildPackageDirectory(build_package)
	if err != nil {
		return err
	}

	// Start the initial build units.
	return invokeBuild(graph, initial_jobs, build_package_directory)
}

func buildUnitConfig(job *buildgraph.Job) *unitconfig.UnitConfig {
	return &unitconfig.UnitConfig{
		Version: 1,
		ContainerArr: []*unitconfig.ContainerSection{
			&unitconfig.ContainerSection{
				Name:       job.Name,
				Dockerfile: job.Dockerfile,
				Registry:   "quay.io/rafecolton", // TODO: read this from the config.
				Project:    job.ImageName,
				Tags:       job.Tags,
				SkipPush:   job.SkipPush,
			},
		},
	}
}

func invokeBuild(graph *buildgraph.Graph, initial_jobs []*buildgraph.Job, build_package_directory string) error {
	current_jobs := initial_jobs
	finished_jobs := make([]*buildgraph.Job, 0)
	broken_jobs := make([]*buildgraph.Job, 0)

	for {
		// Create a channel to collect the done state of each of the jobs to run.
		done := make(chan BuildResultStruct, len(current_jobs))

		// Start each of the build units in their own gorountine. This allows the units to run
		// concurrently.
		log.Printf("Jobs to run: %d", len(current_jobs))
		for _, job := range current_jobs {
			unit_config := buildUnitConfig(job)

			go (func() {
				log.Printf("Starting Job: %s", job.Name)
				job_err := runner.RunBuildSynchronously(unit_config, build_package_directory)
				done <- BuildResultStruct{
					job:       job,
					job_error: job_err,
				}
			})()
		}

		// Wait for each of the jobs to complete. As each job completes, add it to the finished
		// or broken sets, and append any artifacts created.
		for i := 0; i < len(current_jobs); i++ {
			result := <-done
			if result.job_error == nil {
				finished_jobs = append(finished_jobs, result.job)
			} else {
				broken_jobs = append(broken_jobs, result.job)
				log.Printf("Block %s failed with error: %s", result.job.Name, result.job_error)
			}
		}

		// Retrieve the next set of jobs to perform.
		next_jobs, err := graph.GetDependants(finished_jobs, broken_jobs)
		if err != nil {
			return err
		}

		if len(next_jobs) == 0 {
			break
		}

		current_jobs = next_jobs
	}

	if len(broken_jobs) > 0 {
		return errors.New("Early termination due to one or more build units failing")
	}

	return nil
}
