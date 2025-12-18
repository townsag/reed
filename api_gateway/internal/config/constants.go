package config

import (
	"github.com/townsag/reed/api_gateway/internal/util"
)

var UserServiceAddr string = util.GetEnvWithDefault(
	"USER_SERVICE_ADDRESS", "user-service:50051",
)