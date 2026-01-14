package config

import (
	"time"
	"github.com/townsag/reed/api_gateway/internal/util"
)

var UserServiceAddr string = util.GetEnvWithDefault(
	"USER_SERVICE_ADDRESS", "user-service:50051",
)
var DocumentServiceAddr string = util.GetEnvWithDefault(
	"DOCUMENT_SERVICE_ADDRESS", "document-service:50051",
)

const TIMEOUT_MILLISECONDS = 500 * time.Millisecond

var JWTSecretKey string = util.GetEnvWithDefault(
	"JWT_SIGNING_KEY", "asdf",
)