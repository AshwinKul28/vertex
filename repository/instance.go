package repository

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"strings"

	"github.com/google/uuid"
	"github.com/vertex-center/vertex/logger"
	"github.com/vertex-center/vertex/storage"
	"github.com/vertex-center/vertex/types"
)

var (
	ErrContainerNotFound = errors.New("container not found")
)

const (
	EventChange = "change"
)

type InstanceFSRepository struct {
	instances map[uuid.UUID]*types.Instance
	listeners map[uuid.UUID]chan types.InstanceEvent
	observer  chan types.InstanceEvent
}

func NewInstanceFSRepository() InstanceFSRepository {
	r := InstanceFSRepository{
		instances: map[uuid.UUID]*types.Instance{},
		listeners: map[uuid.UUID]chan types.InstanceEvent{},
		observer:  make(chan types.InstanceEvent),
	}

	r.reload()

	go func() {
		defer close(r.observer)

		for {
			<-r.observer
			r.notifyListeners(types.InstanceEvent{
				Name: EventChange,
			})
		}
	}()

	return r
}

func (r *InstanceFSRepository) Get(uuid uuid.UUID) (*types.Instance, error) {
	i := r.instances[uuid]
	if i == nil {
		return nil, fmt.Errorf("the service '%s' is not instances", uuid)
	}
	return i, nil
}

func (r *InstanceFSRepository) GetAll() map[uuid.UUID]*types.Instance {
	return r.instances
}

func (r *InstanceFSRepository) GetPath(uuid uuid.UUID) string {
	return path.Join(storage.PathInstances, uuid.String())
}

func (r *InstanceFSRepository) Delete(uuid uuid.UUID) error {
	err := os.RemoveAll(r.GetPath(uuid))
	if err != nil {
		return fmt.Errorf("failed to delete server uuid=%s: %v", uuid, err)
	}

	delete(r.instances, uuid)

	r.notifyListeners(types.InstanceEvent{
		Name: EventChange,
	})

	return nil
}

func (r *InstanceFSRepository) Exists(uuid uuid.UUID) bool {
	return r.instances[uuid] != nil
}

func (r *InstanceFSRepository) Set(uuid uuid.UUID, instance types.Instance) error {
	if r.Exists(uuid) {
		return fmt.Errorf("the instance '%s' already exists", uuid)
	}

	r.instances[uuid] = &instance
	r.notifyListeners(types.InstanceEvent{
		Name: EventChange,
	})

	instance.Register(r.observer)

	return nil
}

func (r *InstanceFSRepository) AddListener(channel chan types.InstanceEvent) uuid.UUID {
	id := uuid.New()
	r.listeners[id] = channel

	logger.Log("registered to instance").
		AddKeyValue("channel", id).
		Print()

	return id
}

func (r *InstanceFSRepository) RemoveListener(uuid uuid.UUID) {
	delete(r.listeners, uuid)

	logger.Log("unregistered from instance").
		AddKeyValue("channel", uuid).
		Print()
}

func (r *InstanceFSRepository) SaveMetadata(i *types.Instance) error {
	metaPath := path.Join(r.GetPath(i.UUID), ".vertex", "instance_metadata.json")

	metaBytes, err := json.MarshalIndent(i.InstanceMetadata, "", "\t")
	if err != nil {
		return err
	}

	err = os.WriteFile(metaPath, metaBytes, os.ModePerm)
	if err != nil {
		return err
	}

	return nil
}

func (r *InstanceFSRepository) LoadMetadata(i *types.Instance) error {
	metaPath := path.Join(r.GetPath(i.UUID), ".vertex", "instance_metadata.json")
	metaBytes, err := os.ReadFile(metaPath)

	if errors.Is(err, os.ErrNotExist) {
		logger.Log("instance_metadata.json not found. using default.").Print()
	} else if err != nil {
		return err
	} else {
		err = json.Unmarshal(metaBytes, &i.InstanceMetadata)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *InstanceFSRepository) ReadService(instancePath string) (types.Service, error) {
	data, err := os.ReadFile(path.Join(instancePath, ".vertex", "service.json"))
	if err != nil {
		logger.Warn("service has no '.vertex/service.json' file").
			AddKeyValue("path", path.Dir(instancePath)).
			Print()
	}

	var service types.Service
	err = json.Unmarshal(data, &service)
	return service, err
}

func (r *InstanceFSRepository) SaveEnv(i *types.Instance, variables map[string]string) error {
	filepath := path.Join(r.GetPath(i.UUID), ".env")

	file, err := os.OpenFile(filepath, os.O_WRONLY|os.O_CREATE, os.ModePerm)
	if err != nil {
		return err
	}

	for key, value := range variables {
		_, err := file.WriteString(strings.Join([]string{key, value}, "=") + "\n")
		if err != nil {
			return err
		}
	}

	i.EnvVariables.Entries = variables

	return nil
}

func (r *InstanceFSRepository) LoadEnv(i *types.Instance) error {
	filepath := path.Join(r.GetPath(i.UUID), ".env")

	file, err := os.Open(filepath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.Split(scanner.Text(), "=")
		if len(line) < 2 {
			return errors.New("failed to read .env")
		}

		i.EnvVariables.Entries[line[0]] = line[1]
	}

	return nil
}

func (r *InstanceFSRepository) notifyListeners(event types.InstanceEvent) {
	for _, listener := range r.listeners {
		listener <- event
	}
}

func (r *InstanceFSRepository) Close() {
	for _, instance := range r.instances {
		instance.Logger.CloseLogFile()
		err := instance.UptimeStorage.Close()
		if err != nil {
			logger.Error(err).Print()
		}
	}
}

func (r *InstanceFSRepository) reload() {
	r.Close()

	entries, err := os.ReadDir(storage.PathInstances)
	if err != nil {
		log.Fatal(err)
	}

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			log.Fatal(err)
		}

		isInstance := entry.IsDir() || info.Mode()&os.ModeSymlink != 0

		if isInstance {
			logger.Log("found service").
				AddKeyValue("uuid", entry.Name()).
				Print()

			id, err := uuid.Parse(entry.Name())
			if err != nil {
				log.Fatal(err)
			}

			_, err = r.Load(id)
			if err != nil {
				log.Fatal(err)
			}
		}
	}
}

func (r *InstanceFSRepository) Load(uuid uuid.UUID) (*types.Instance, error) {
	instancePath := path.Join(storage.PathInstances, uuid.String())

	service, err := r.ReadService(instancePath)
	if err != nil {
		return nil, err
	}

	instance, err := types.NewInstance(uuid, service, instancePath)
	if err != nil {
		return nil, err
	}

	err = r.LoadMetadata(&instance)
	if err != nil {
		return nil, err
	}

	err = r.Set(uuid, instance)
	if err != nil {
		return nil, err
	}

	return &instance, nil
}
