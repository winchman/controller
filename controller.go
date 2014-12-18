package buildcontroller

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/Sirupsen/logrus"
	"github.com/docker/docker/pkg/archive"
	"github.com/winchman/artifactory"
	"github.com/winchman/builder-core"
	"github.com/winchman/builder-core/unit-config"
	"github.com/winchman/graph-builder"
	"io/ioutil"
	"log"
	"os"
	"path"
)

const (
	artifactsInPathTemplate = ".winchman/in/%s/"
	artifactsOutPath        = ".winchman/out/"
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
	imageID  string
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
		projectName = options.ProjectName
		if job.PushImage {
			return nil, fmt.Errorf("Missing ImageName for PushImage unit %s", job.Name)
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
				SkipPush:   !job.PushImage,
				CfgUn:      username,
				CfgPass:    password,
			},
		},
	}, nil
}

func runBuild(unitConfig *unitconfig.UnitConfig, buildPackageDirectory string) (string, error) {
	opts := runner.Options{
		UnitConfig: unitConfig,
		ContextDir: buildPackageDirectory,
		LogLevel:   logrus.InfoLevel,
	}

	var imageID = ""
	var logger = logrus.New()
	logger.Level = opts.LogLevel
	log, status, done := runner.RunBuild(opts)
	for {
		select {
		case e, ok := <-log:
			if !ok {
				return imageID, errors.New("log channel closed prematurely")
			}

			if currentimageID, exists := e.Entry().Data["image_id"]; exists {
				imageID = currentimageID.(string)
			}

			e.Entry().Logger = logger
			switch e.Entry().Level {
			case logrus.PanicLevel:
				e.Entry().Panicln(e.Entry().Message)
			case logrus.FatalLevel:
				e.Entry().Fatalln(e.Entry().Message)
			case logrus.ErrorLevel:
				e.Entry().Errorln(e.Entry().Message)
			case logrus.WarnLevel:
				e.Entry().Warnln(e.Entry().Message)
			case logrus.InfoLevel:
				e.Entry().Infoln(e.Entry().Message)
			default:
				e.Entry().Debugln(e.Entry().Message)
			}
		case event, ok := <-status:
			if !ok {
				return imageID, errors.New("status channel closed prematurely")
			}
			logger.WithFields(event.Data()).Debugf("status event (type %s)", event.EventType())
		case err, ok := <-done:
			if !ok {
				return imageID, errors.New("exit channel closed prematurely")
			}
			return imageID, err
		}
	}
}

func appendBuiltArtifacts(unitName string, imageID string, buildPackageDirectory string) error {
	tempDir, err := ioutil.TempDir("", "artifacts-"+imageID)
	if err != nil {
		return err
	}

	log.Printf("Copying artifacts from %s (image ID %s)", unitName, imageID)

	// Copy the 'out' directory from the container into a TAR in the temp directory.
	// TODO(jschorr): Change this to just create a TAR without the 'out' prefix once the artifactory
	// supports that use case.
	opts := artifactory.NewResourceOptions{
		StorageDir: tempDir,
		Handle:     imageID,
		Path:       artifactsOutPath,
	}

	var resource = artifactory.NewResource(opts)

	defer func() {
		_ = resource.Reset()
		_ = os.RemoveAll(tempDir)
	}()

	artifactBytes, err := resource.ArtifactBytes()
	if err != nil {
		return err
	}

	// Untar the artifacts into the temp directory.
	byteReader := bytes.NewReader(artifactBytes)
	err = archive.Untar(byteReader, tempDir, &archive.TarOptions{NoLchown: true})
	if err != nil {
		return err
	}

	// Make the artifacts 'in' directory.
	inPath := fmt.Sprintf(artifactsInPathTemplate, unitName)
	contextPath := path.Join(buildPackageDirectory, inPath)

	err = os.MkdirAll(path.Dir(contextPath), 0777)
	if err != nil {
		return err
	}

	// Move the contents of the 'out' directory up one level, since we don't want 'out'
	// in the final directory structure.
	return os.Rename(path.Join(tempDir, "out"), contextPath)
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
				imageID, jobError := runBuild(unitConfig, buildPackageDirectory)

				if jobError == nil {
					log.Printf("Job Completed with image id %s", imageID)
				}

				done <- buildResultStruct{
					job:      job,
					jobError: jobError,
					imageID:  imageID,
				}
			}()
		}

		// Wait for each of the jobs to complete. As each job completes, add it to the finished
		// or broken sets.
		var successfulJobs []buildResultStruct
		for i := 0; i < len(currentJobs); i++ {
			result := <-done
			if result.jobError != nil {
				brokenJobs = append(brokenJobs, result.job)
				log.Printf("Block %s failed with error: %s", result.job.Name, result.jobError)
				continue
			}

			finishedJobs = append(finishedJobs, result.job)
			successfulJobs = append(successfulJobs, result)
		}

		// For each successful job, append the artifacts created and placed into the output
		// directory. We do this here (rather than in the loop above), as builds may still
		// be using the build context directory until we reach this point.
		log.Printf("Copying artifacts for %d successful jobs", len(successfulJobs))
		for i := 0; i < len(successfulJobs); i++ {
			jobInfo := successfulJobs[i]
			if jobInfo.imageID != "" {
				if appendError := appendBuiltArtifacts(jobInfo.job.Name, jobInfo.imageID, buildPackageDirectory); appendError != nil {
					return appendError
				}
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
