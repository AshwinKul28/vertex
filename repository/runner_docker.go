package repository

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"path"
	"path/filepath"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/archive"
	"github.com/docker/go-connections/nat"
	"github.com/vertex-center/vertex/pkg/logger"
	"github.com/vertex-center/vertex/pkg/storage"
	"github.com/vertex-center/vertex/types"
)

type RunnerDockerRepository struct {
	cli *client.Client
}

type dockerMessage struct {
	Stream string `json:"stream"`
}

func NewRunnerDockerRepository() RunnerDockerRepository {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		logger.Warn("couldn't connect with the Docker cli.").
			AddKeyValue("error", err.Error()).
			Print()

		return RunnerDockerRepository{}
	}

	return RunnerDockerRepository{
		cli: cli,
	}
}

func (r RunnerDockerRepository) Delete(instance *types.Instance) error {
	id, err := r.getID(*instance)
	if err != nil {
		return err
	}

	return r.cli.ContainerRemove(context.Background(), id, dockertypes.ContainerRemoveOptions{})
}

func (r RunnerDockerRepository) Start(instance *types.Instance, onLog func(msg string), onErr func(msg string), setStatus func(status string)) error {
	imageName := instance.DockerImageName()
	containerName := instance.DockerContainerName()

	setStatus(types.InstanceStatusBuilding)

	instancePath := path.Join(storage.PathInstances, instance.UUID.String())

	// Build
	var err error
	if instance.Methods.Docker.Dockerfile != nil {
		err = r.buildImageFromDockerfile(instancePath, imageName, onLog)
	} else if instance.Methods.Docker.Image != nil {
		err = r.buildImageFromName(*instance.Methods.Docker.Image, onLog)
	} else {
		err = errors.New("no Docker methods found")
	}

	if err != nil {
		onErr(err.Error())
		return err
	}

	// Create
	id, err := r.getID(*instance)
	if err == ErrContainerNotFound {
		logger.Log("container doesn't exists, create it.").
			AddKeyValue("container_name", instance.DockerContainerName()).
			Print()

		exposedPorts := nat.PortSet{}
		portBindings := nat.PortMap{}
		if instance.Methods.Docker.Ports != nil {
			var all []string

			for _, out := range *instance.Methods.Docker.Ports {
				in := ""
				for _, e := range instance.EnvDefinitions {
					if e.Type == "port" && e.Default == out {
						in = instance.EnvVariables[e.Name]
						all = append(all, in+":"+out)
						break
					}
				}
			}

			var err error
			exposedPorts, portBindings, err = nat.ParsePortSpecs(all)
			if err != nil {
				return err
			}
		}

		var binds []string
		if instance.Methods.Docker.Volumes != nil {
			for source, target := range *instance.Methods.Docker.Volumes {
				source, err = filepath.Abs(path.Join(instancePath, "volumes", source))
				if err != nil {
					return err
				}
				binds = append(binds, source+":"+target)
			}
		}

		if instance.Methods.Docker.Dockerfile != nil {
			id, err = r.createContainer(imageName, containerName, exposedPorts, portBindings, binds)
		} else if instance.Methods.Docker.Image != nil {
			id, err = r.createContainer(*instance.Methods.Docker.Image, instance.DockerContainerName(), exposedPorts, portBindings, binds)
		}
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	// Start
	err = r.cli.ContainerStart(context.Background(), id, dockertypes.ContainerStartOptions{})
	if err != nil {
		setStatus(types.InstanceStatusError)
		return err
	}

	setStatus(types.InstanceStatusRunning)
	return nil
}

func (r RunnerDockerRepository) Stop(instance *types.Instance) error {
	id, err := r.getID(*instance)
	if err != nil {
		return err
	}

	return r.cli.ContainerStop(context.Background(), id, container.StopOptions{})
}

func (r RunnerDockerRepository) Info(instance types.Instance) (map[string]any, error) {
	id, err := r.getID(instance)
	if err != nil {
		return nil, err
	}

	info, err := r.cli.ContainerInspect(context.Background(), id)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"id":       info.ID,
		"name":     info.Name,
		"image":    info.Image,
		"platform": info.Platform,
	}, nil
}

func (r RunnerDockerRepository) getID(instance types.Instance) (string, error) {
	containers, err := r.cli.ContainerList(context.Background(), dockertypes.ContainerListOptions{
		All: true,
	})
	if err != nil {
		return "", err
	}

	var containerID string

	for _, c := range containers {
		name := c.Names[0]
		if name == "/"+instance.DockerContainerName() {
			containerID = c.ID
			break
		}
	}

	if containerID == "" {
		return "", ErrContainerNotFound
	}

	return containerID, nil
}

func (r RunnerDockerRepository) buildImageFromName(imageName string, onMsg func(msg string)) error {
	res, err := r.cli.ImagePull(context.Background(), imageName, dockertypes.ImagePullOptions{})
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(res)
	for scanner.Scan() {
		if scanner.Err() != nil {
			return scanner.Err()
		}
		onMsg(scanner.Text())
	}

	return nil
}

func (r RunnerDockerRepository) buildImageFromDockerfile(instancePath string, imageName string, onMsg func(msg string)) error {
	buildOptions := dockertypes.ImageBuildOptions{
		Dockerfile: "Dockerfile",
		Tags:       []string{imageName},
		Remove:     true,
	}

	reader, err := archive.TarWithOptions(instancePath, &archive.TarOptions{
		ExcludePatterns: []string{".git/**/*"},
	})
	if err != nil {
		return err
	}

	res, err := r.cli.ImageBuild(context.Background(), reader, buildOptions)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	scanner := bufio.NewScanner(res.Body)
	for scanner.Scan() {
		if scanner.Err() != nil {
			return scanner.Err()
		}
		msg := dockerMessage{}
		err := json.Unmarshal(scanner.Bytes(), &msg)
		if err != nil {
			logger.Warn("Failed to parse message:").
				AddKeyValue("msg", scanner.Text()).
				Print()
		} else {
			if msg.Stream != "" {
				onMsg(msg.Stream)
			}
		}
	}

	logger.Log("Docker build: success.").Print()
	return nil
}

func (r RunnerDockerRepository) createContainer(imageName string, containerName string, exposedPorts nat.PortSet, portBindings nat.PortMap, binds []string) (string, error) {
	config := container.Config{
		Image:        imageName,
		ExposedPorts: exposedPorts,
	}

	hostConfig := container.HostConfig{
		Binds:        binds,
		PortBindings: portBindings,
	}

	res, err := r.cli.ContainerCreate(context.Background(), &config, &hostConfig, nil, nil, containerName)
	for _, warn := range res.Warnings {
		logger.Warn("warning while creating container").
			AddKeyValue("warning", warn).
			Print()
	}
	return res.ID, err
}
