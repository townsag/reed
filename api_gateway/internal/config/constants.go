package config

import (
	"time"
	"github.com/townsag/reed/api_gateway/internal/util"
)

var UserServiceAddr string = util.GetEnvWithDefault(
	"USER_SERVICE_ADDRESS", "user-service:50051",
)

const TIMEOUT_MILLISECONDS = 500 * time.Millisecond