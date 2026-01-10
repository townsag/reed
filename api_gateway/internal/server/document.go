package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	"google.golang.org/protobuf/proto"

	"github.com/google/uuid"
	pb "github.com/townsag/reed/document_service/api/v1"
)

/*
Notes:
- these are the concerns of the api-gateway layer
	- authentication: who are they
	- orchestration: make calls to a number of internal services, compose responses
- these are not the concerns of the api-gateway layer
	- authorization: what are they allowed to do

- as a result of this distinction, the api-gateway should not have any permission
  related business logic, that is the concern of the document service. Leave all
  validation of request permissions to the document service
	- examples: can this principal call the create document route, can this principal
	  call the delete documents route

- the client does not need to modify or read the contents of the cursor
- we can base 64 encode the cursor and send it to the client as part of the response
	- if the client sends us back the base 64 encoded cursor as part of their next
	  request then we know to pick up where they left off
*/

func serverToClientPermissionLevel(permissionLevel PermissionLevel) (pb.PermissionLevel, error) {
	switch permissionLevel {
	case Owner:
		return pb.PermissionLevel_PERMISSION_OWNER, nil
	case Editor:
		return pb.PermissionLevel_PERMISSION_EDITOR, nil
	case Viewer:
		return pb.PermissionLevel_PERMISSION_VIEWER, nil
	}
	return -1, fmt.Errorf("failed to map the permission level to a valid proto type")
}

func serverToClientCursor(cursor string) (*pb.Cursor, error) {
	// decode the url safe base64 cursor back to the protobuf wire format
	wire, err := base64.URLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to decode the base64 cursor representation ",
			"to proto wire format with error: %w", err,
		)
	}
	// unmarshal the proto struct from the wire format
	var pbCursor pb.Cursor
	err = proto.Unmarshal(wire, &pbCursor)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to unmarshal the wire format of the cursor with error: %w", err,
		)
	}
	return &pbCursor, nil
}

func clientToServerCursor(cursor *pb.Cursor) (string, error) {
	// serialize the struct to the protobuf wire format
	wire, err := proto.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf(
			"failed to serialize the protobuf cursor to the",
			" wire format with error: %w", err,
		)
	}
	// serialize the wire format byte array to a url safe base64 string
	cursorString := base64.URLEncoding.EncodeToString(wire)
	return cursorString, nil
}

func clientToServerDocument(document *pb.Document) (*Document, error) {
	// parse the document id
	documentId, err := uuid.Parse(document.DocumentId)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to parse the returned document id with error: %w", err,
		)
	}
	return &Document{
		CreatedAt: document.CreatedAt.Seconds,
		DocumentDescription: document.Description,
		DocumentId: documentId,
		DocumentName: document.DocumentName,
		LastModifiedAt: document.LastModifiedAt.Seconds,
	}, nil
}

// func clientToServerDocuments()

// batch delete endpoint for deleting lists of documents
// (DELETE /document)
func (s *Service) DeleteDocument(w http.ResponseWriter, r *http.Request) {
	// parse the request body 
	var reqBody DeleteDocumentJSONRequestBody
	err := json.NewDecoder(r.Body).Decode(&reqBody)
	if err != nil {
		SendError(w, http.StatusBadRequest, err.Error())
		return
	}
	// read the JWT claims from the request context
	claims, err := GetClaims(r.Context())
	if err != nil {
		SendError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	// don't check if the given token has the right permissions in the api gateway
	// push down all business logic to the document service. The document service
	// will be able to tell if the given principal is a guest or a user and if 
	// it has the correct permissions
	principalId, err := claims.ParsePrincipalId()
	if err != nil {
		SendError(w, http.StatusBadRequest, err.Error())
		return
	}
	// coarse grain authorization check: only users should be able to delete document
	// check if the token is not user type, if so, return an error
	principalType := claims.GetTokenType()
	if principalType != UserType {
		SendError(w, http.StatusForbidden,
			fmt.Sprintf("Only user type tokens can delete documents, received token with type: %s", principalType),
		)
		return
	}
	// call the document microservice with these document ids
	// if the principal id is a guest id, the document service will reject it
	err = s.documentServiceClient.DeleteDocuments(r.Context(), reqBody.DocumentIds, principalId)
	if err != nil {
		SendError(w, GrpcToHttpStatus(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// get all the documents that a given user has owner permissions on
// (GET /document)
func (s *Service) GetDocument(w http.ResponseWriter, r *http.Request, params GetDocumentParams) {
	// read the JWT claims from the request context
	claims, err := GetClaims(r.Context())
	if err != nil {
		SendError(w, http.StatusInternalServerError, "Internal Server Error")
		return 
	}
	// parse the principle id from the JWT claims 
	principalId, err := claims.ParsePrincipalId()
	if err != nil {
		SendError(w, http.StatusBadRequest, err.Error())
		return
	}
	// cursor, limit, and permission level are query params in the params struct
	// if the cursor is present, reformat the cursor so that it can be passed to the document
	// service client
	var cursor *pb.Cursor = nil
	if params.Cursor != nil {
		cursor, err = serverToClientCursor(*params.Cursor)
		if err != nil {
			SendError(w, http.StatusBadRequest, "failed to parse the provided cursor")
			return
		}
	}
	// use the owner permission level if the permission level is not present in the get document
	// params struct
	var permissionLevel pb.PermissionLevel
	if params.PermissionLevel == nil {
		permissionLevel = pb.PermissionLevel_PERMISSION_OWNER
	} else {
		parsedPermissionLevel, err := serverToClientPermissionLevel(*params.PermissionLevel)
		if err != nil {
			SendError(w, http.StatusBadRequest, err.Error())
			return
		} else {
			permissionLevel = parsedPermissionLevel
		}
	}
	// if the limit is not present, we pass nil for the limit and let the document service define 
	// the default value
	// call the document service client 
	reply, err := s.documentServiceClient.ListDocumentsByPrincipal(
		r.Context(),
		principalId,		// target principal id 
		principalId,		// calling principal id
		[]pb.PermissionLevel{permissionLevel},
		cursor,
		params.Limit,
	)
	if err != nil {
		SendError(w, GrpcToHttpStatus(err), err.Error())
		return
	}
	// format the document service response into the http response
	// format a cursor for the documents response 
	respCursor, err := clientToServerCursor(reply.Cursor)
	if err != nil {
		SendError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	// format a list of documents from the document permissions structs in the responses
	var documents []Document = make([]Document, len(reply.DocumentPermissions))
	for i, documentPermission := range reply.DocumentPermissions {
		document, err := clientToServerDocument(documentPermission.Document)
		if err != nil {
			SendError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		documents[i] = *document
	}
	response := &GetDocumentResponse{
		Cursor: &respCursor,
		Documents: documents,
	}
	SendJsonResponse(w, http.StatusOK, response)
}

// create a new document for a user
// (POST /document)
func (s *Service) PostDocument(w http.ResponseWriter, r *http.Request) {
	// parse the request body
	var request PostDocumentJSONRequestBody
	err := json.NewDecoder(r.Body).Decode(&request)
	if err != nil {
		SendError(w, http.StatusBadRequest, fmt.Sprintf(
			"failed to parse the request body with error: %v", err.Error(),
		))
		return
	}
	// read the jwt claims from the request context
	claims, err := GetClaims(r.Context())
	if err != nil {
		// we use an internal server error here because all requests should have a
		// claims struct in the request context that has been populated by the auth
		// middleware. If it is missing, that means the middleware is broken
		SendError(w, http.StatusInternalServerError, "InternalServerError")
		return
	}
	// validate that the token is a user type token, guests should not be able to
	// create documents 
	if claims.GetTokenType() != UserType {
		SendError(w, http.StatusForbidden, "must have a user type token to make documents")
		return
	}
	// coarse grain authorization
	userId, err := claims.ParsePrincipalId()
	if err != nil {
		SendError(w, http.StatusBadRequest, err.Error())
		return
	}
	// call the document service with the document information parsed from
	// the request body and the user id parsed from the JWT claims
	resp, err := s.documentServiceClient.CreateDocument(
		r.Context(),
		userId,
		request.DocumentName,
		request.DocumentDescription,
	)
	// if the call fails, proxy the error back to the client
	if err != nil {
		SendError(w, GrpcToHttpStatus(err), err.Error())
		return
	}
	// if the call succeeds, proxy the created document's documentId back to the client
	documentId, err := uuid.Parse(resp.DocumentId)
	if err != nil {
		SendError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	SendJsonResponse(
		w, http.StatusOK, &PostDocumentResponse{
			DocumentId: documentId,
		},
	)
}

// delete a document
// (DELETE /document/{documentId})
func (s *Service) DeleteDocumentDocumentId(w http.ResponseWriter, r *http.Request, documentId DocumentId) {
	// document id is a query parameter that has been parsed out of the request path
	// parse the userId from the custom claims 
	claims, err := GetClaims(r.Context())
	if err != nil {
		SendError(w, http.StatusInternalServerError, "Internal server error")
		return
	}
	principalId, err := claims.ParsePrincipalId()
	if err != nil {
		SendError(w, http.StatusBadRequest, err.Error())
		return
	}
	// coarse grain authorization, check if the type of the token is user type 
	// if not, return an error 
	if claims.GetTokenType() != UserType {
		SendError(w, http.StatusForbidden, "must have a user type token to delete documents")
		return
	}
	// call the document service with the userId and the documentId
	err = s.documentServiceClient.DeleteDocument(
		r.Context(), documentId, principalId,
	)
	if err != nil {
		SendError(w, GrpcToHttpStatus(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// get one document
// (GET /document/{documentId})
func (s *Service) GetDocumentDocumentId(w http.ResponseWriter, r *http.Request, documentId DocumentId) {
	// document Id is a query parameter that has been parsed out of the request path
	// parse the userId from the custom claims
	claims, err := GetClaims(r.Context())
	if err != nil {
		SendError(w, http.StatusInternalServerError, "Internal Server error")
		return
	}
	principalId, err := claims.ParsePrincipalId()
	if err != nil {
		SendError(w, http.StatusBadRequest, err.Error())
		return
	}
	// call the document service with the document id and the user id
	result, err := s.documentServiceClient.GetDocument(r.Context(), principalId, documentId)
	if err != nil {
		SendError(w, GrpcToHttpStatus(err), err.Error())
		return
	}
	// format the document service response such that it can be sent as an http response body
	document, err := clientToServerDocument(result.Document)
	if err != nil {
		SendError(
			w, http.StatusInternalServerError,
			"Internal server error, failed to parse document sent from document service",
		)
		return
	}
	SendJsonResponse(w, http.StatusOK, document)
}

// update one document
// (PUT /document/{documentId})
func (s *Service) PutDocumentDocumentId(w http.ResponseWriter, r *http.Request, documentId DocumentId) {
	// parse the request body
	var body PutDocumentDocumentIdJSONRequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		SendError(w, http.StatusBadRequest, err.Error())
		return
	}
	// parse the claims from the JWT in the request Authorization header
	claims, err := GetClaims(r.Context())
	if err != nil {
		SendError(w, http.StatusInternalServerError, "Internal server error")
	}
	// parse the principal id from the token
	principalId, err := claims.ParsePrincipalId()
	if err != nil {
		SendError(w, http.StatusBadRequest, err.Error())
	}
	// call the document service using the document service client
	err = s.documentServiceClient.UpdateDocument(
		r.Context(), documentId, principalId, body.DocumentName, body.DocumentDescription,
	)
	// proxy any error back to the client
	if err != nil {
		SendError(w, GrpcToHttpStatus(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}