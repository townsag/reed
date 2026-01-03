package server

import (
	"net/http"
)

// create a middleware function that intercepts domain errors raised from the service 
// layer and translates them to appropriate
// consider refactoring this function to use the factory pattern instead so that 
// we can use closures to make the error handler more configurable
func ErrorHandlerFunc(w http.ResponseWriter, r *http.Request, err error) {
	http.Error(w, err.Error(), http.StatusBadRequest)
}
// my understanding is that this function is used to handle errors that the generated
// code encounters when parsing request query parameters and path parameters