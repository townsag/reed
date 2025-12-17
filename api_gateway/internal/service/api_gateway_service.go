package service

import (
	"github.com/townsag/reed/api_gateway/internal/server"
)

var _ server.ServerInterface = (*Service)(nil)

type Service struct {

}

func NewService() Service {
	return Service{}
}