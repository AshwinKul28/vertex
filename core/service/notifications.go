package service

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/webhook"
	"github.com/google/uuid"
	"github.com/vertex-center/vertex/apps/containers/core/types"
	"github.com/vertex-center/vertex/core/port"
	types2 "github.com/vertex-center/vertex/core/types"
)

// TODO: Move webhooks use to a Discord adapter

type NotificationsService struct {
	uuid            uuid.UUID
	ctx             *types2.VertexContext
	settingsAdapter port.SettingsAdapter
	client          webhook.Client
}

func NewNotificationsService(ctx *types2.VertexContext, settingsAdapter port.SettingsAdapter) NotificationsService {
	return NotificationsService{
		uuid:            uuid.New(),
		ctx:             ctx,
		settingsAdapter: settingsAdapter,
	}
}

func (s *NotificationsService) StartWebhook() error {
	webhookURL := s.settingsAdapter.GetNotificationsWebhook()
	if webhookURL == nil {
		return nil
	}

	var err error
	s.client, err = webhook.NewWithURL(*webhookURL)
	if err != nil {
		return err
	}

	s.ctx.AddListener(s)

	return nil
}

func (s *NotificationsService) StopWebhook() {
	s.ctx.RemoveListener(s)
}

func (s *NotificationsService) GetUUID() uuid.UUID {
	return s.uuid
}

func (s *NotificationsService) OnEvent(e interface{}) {
	switch e := e.(type) {
	case types.EventContainerStatusChange:
		if e.Status == types.ContainerStatusOff || e.Status == types.ContainerStatusError || e.Status == types.ContainerStatusRunning {
			s.sendStatus(e.Name, e.Status)
		}
	}
}

func (s *NotificationsService) sendStatus(name string, status string) {
	var color int

	switch status {
	case types.ContainerStatusRunning:
		color = 5763719
	case types.ContainerStatusOff:
		color = 15548997
	case types.ContainerStatusError:
		color = 10038562
	}

	embed := discord.NewEmbedBuilder().
		SetTitle(name).
		SetDescriptionf("Status: %s", status).
		SetColor(color).
		Build()

	_, err := s.client.CreateEmbeds([]discord.Embed{embed})
	if err != nil {
		return
	}
}
