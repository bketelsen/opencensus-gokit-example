package twirp

import (
	// stdlib
	"context"
	"io"
	"net/http"

	// external
	"github.com/go-kit/kit/log"
	kitsd "github.com/go-kit/kit/sd"
	"github.com/satori/go.uuid"

	// project
	"github.com/basvanbeek/opencensus-gokit-example/services/event"
	"github.com/basvanbeek/opencensus-gokit-example/services/event/transport/pb"
	"github.com/basvanbeek/opencensus-gokit-example/shared/sd"
)

type client struct {
	instancer func() pb.Event
	logger    log.Logger
}

// NewClient returns a new event client using the Twirp transport.
func NewClient(instancer kitsd.Instancer, c *http.Client, logger log.Logger) event.Service {
	if c == nil {
		c = &http.Client{}
	}
	return &client{
		instancer: factory(instancer, c, logger),
		logger:    logger,
	}
}

func (c client) Create(
	ctx context.Context, tenantID uuid.UUID, event event.Event,
) (*uuid.UUID, error) {
	ci := c.instancer()
	if ci == nil {
		return nil, sd.ErrNoClients
	}

	res, err := ci.Create(ctx, &pb.CreateRequest{
		TenantId: tenantID.Bytes(),
		Event: &pb.EventObj{
			Id:   event.ID.Bytes(),
			Name: event.Name,
		},
	})
	if err != nil {
		return nil, err
	}

	id, err := uuid.FromBytes(res.Id)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func (c client) Get(
	ctx context.Context, tenantID, id uuid.UUID,
) (*event.Event, error) {
	ci := c.instancer()
	if ci == nil {
		return nil, sd.ErrNoClients
	}

	res, err := ci.Get(ctx, &pb.GetRequest{
		TenantId: tenantID.Bytes(),
		Id:       id.Bytes(),
	})
	if err != nil {
		return nil, err
	}
	return &event.Event{
		ID:   uuid.FromBytesOrNil(res.Event.Id),
		Name: res.Event.Name,
	}, nil
}

func (c client) Update(
	ctx context.Context, tenantID uuid.UUID, event event.Event,
) error {
	ci := c.instancer()
	if ci == nil {
		return sd.ErrNoClients
	}

	_, err := ci.Update(ctx, &pb.UpdateRequest{
		TenantId: tenantID.Bytes(),
		Event: &pb.EventObj{
			Id:   event.ID.Bytes(),
			Name: event.Name,
		},
	})

	return err
}

func (c client) Delete(
	ctx context.Context, tenantID uuid.UUID, id uuid.UUID,
) error {
	ci := c.instancer()
	if ci == nil {
		return sd.ErrNoClients
	}

	_, err := ci.Delete(ctx, &pb.DeleteRequest{
		TenantId: tenantID.Bytes(),
		Id:       id.Bytes(),
	})

	return err
}

func (c client) List(
	ctx context.Context, tenantID uuid.UUID,
) ([]*event.Event, error) {
	ci := c.instancer()
	if ci == nil {
		return nil, sd.ErrNoClients
	}

	pbListResponse, err := ci.List(ctx, &pb.ListRequest{
		TenantId: tenantID.Bytes(),
	})
	if err != nil {
		return nil, err
	}
	events := make([]*event.Event, 0, len(pbListResponse.Events))
	for _, evt := range pbListResponse.Events {
		events = append(events, &event.Event{
			ID:   uuid.FromBytesOrNil(evt.Id),
			Name: evt.Name,
		})
	}
	return events, nil
}

func factory(instancer kitsd.Instancer, client *http.Client, logger log.Logger) func() pb.Event {
	factoryFunc := func(instance string) (interface{}, io.Closer, error) {
		return pb.NewEventProtobufClient(instance, client), nil, nil
	}
	clientInstancer := sd.NewClientInstancer(instancer, factoryFunc, logger)
	balancer := sd.NewRoundRobin(clientInstancer)

	return func() pb.Event {
		client, err := balancer.Client()
		if err != nil {
			logger.Log("err", err)
			return nil
		}
		return client.(pb.Event)
	}
}
