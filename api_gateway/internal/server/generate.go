package server

//go:generate go tool oapi-codegen -config ./cfg.yml ../../api/v1/api-gateway.yml

// this should not be necessary but I guess it is 
var _ int = 1