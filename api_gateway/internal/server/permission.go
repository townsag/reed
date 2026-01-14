package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	pb "github.com/townsag/reed/document_service/api/v1"
)

// get all the users that have permission on a document, this is only meant to be called by
// users that have owner permissions on that document
// (GET /document/{documentId}/permission)
func (s *Service) GetDocumentDocumentIdPermission(
	w http.ResponseWriter, 
	r *http.Request, 
	documentId DocumentId,
	params GetDocumentDocumentIdPermissionParams,
) {
	// parse the claims out of the context 
	claims, err := GetClaims(r.Context())
	if err != nil {
		SendError(w, http.StatusInternalServerError, "Internal server error")
		return
	}
	// coarse grain authorization check: only users should be able to call this route 
	// because only users can have owner permissions on documents
	if claims.GetTokenType() != PrincipalTypeUser {
		SendError(w, http.StatusForbidden, "Must have a user type token to list permissions on a document")
		return
	}
	// parse out the calling userId
	userId, err := claims.ParsePrincipalId()
	if err != nil {
		SendError(w, http.StatusBadRequest, err.Error())
		return
	}
	// parse out the permissions filter
	var permissionFilter []pb.PermissionLevel
	if params.PermissionFilter == nil {
		permissionFilter = make([]pb.PermissionLevel, 0)
	} else {
		permissionFilter, err = netToProtoPermissionFilter(*params.PermissionFilter)
		if err != nil {
			SendError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	// parse out the cursor
	var cursor *pb.Cursor = nil
	if params.Cursor != nil {
		cursor, err = netToProtoCursor(*params.Cursor)
		if err != nil {
			SendError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	// call the document service with the document id and the calling users user id
	result, err := s.documentServiceClient.ListPermissionsOnDocument(
		r.Context(), documentId, userId, permissionFilter, cursor, params.Limit,
	)
	if err != nil {
		SendError(w, GrpcToHttpStatus(err), err.Error())
		return
	}
	// reformat the response and send it to the client
	// parse the list of permissions
	permissions, err := protoToNetPermissions(result.RecipientPermissions)
	if err != nil {
		SendError(w, http.StatusInternalServerError, 
			"failed to parse permission returned from backend service",
		)
		return
	}
	// parse the cursor
	responseCursor, err := protoToNetCursor(result.Cursor)
	if err != nil {
		SendError(w, http.StatusInternalServerError, 
			"failed to parse cursor returned from backend service",
		)
		return
	}
	SendJsonResponse(
		w, http.StatusOK,
		&ListPermissionsOnDocumentResponse{
			Cursor: &responseCursor,
			Permissions: permissions,
		},
	)
}

/*
- types of errors that can be returned
	- 400
	- 401
	- 403
	- 404 document not found
	- 404 target user not found
- the 
*/
// create a permission on a document either by sharing the document with an existing user or creating a new guest user for that document
// (POST /document/{documentId}/permission)
func (s *Service) PostDocumentDocumentIdPermission(
	w http.ResponseWriter, r *http.Request, documentId DocumentId,
) {
	// parse the claims from the request context
	claims, err := GetClaims(r.Context())
	if err != nil {
		SendError(w, http.StatusInternalServerError, "Internal Server error")
		return
	}
	// perform a coarse grain check of the provided token, if it is a guest token we can reject it
	principalId, err := claims.ParsePrincipalId()
	if err != nil {
		SendError(w, http.StatusBadRequest, err.Error())
		return
	}
	if claims.GetTokenType() != PrincipalTypeUser {
		SendError(w, http.StatusForbidden, "must have a user token to create permissions on a document")
		return
	}
	// parse the request body
	var reqBody PostDocumentDocumentIdPermissionJSONBody
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		SendError(w, http.StatusBadRequest, err.Error())
		return
	}
	// validate that the permission level is not owner
	if reqBody.PermissionLevel == Owner {
		SendError(w, http.StatusBadRequest, "cannot create permissions at owner level")
		return
	}
	// parse the permission level
	permissionLevel, err := netToProtoPermissionLevel(reqBody.PermissionLevel)
	if err != nil {
		SendError(w, http.StatusBadRequest, "unable to map the given permission level to a valid permission level")
		return
	}
	// determine if this is a request to create a guest or a request to create a permission of a user
	if reqBody.UserIdToShare != nil {
		// this is a request to create a permission on a user
		err := s.documentServiceClient.UpsertPermissionUser(
			r.Context(), *reqBody.UserIdToShare, principalId, documentId, permissionLevel,
		)
		if err != nil {
			SendError(w, GrpcToHttpStatus(err), err.Error())
			return
		}
		// send a response with the user id that the document was shared with
		SendJsonResponse(w, http.StatusOK, &ShareDocumentResponse{
			UserIdSharedWith: reqBody.UserIdToShare,
		})
		return
	} else {
		// this is a request to create a guest
		result, err := s.documentServiceClient.CreateGuest(
			r.Context(), documentId, principalId, permissionLevel,
		)
		if err != nil {
			SendError(w, GrpcToHttpStatus(err), err.Error())
			return
		}
		guestId, err := uuid.Parse(result.GuestId)
		if err != nil {
			SendError(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}
		// send a response with the created guest id
		SendJsonResponse(w, http.StatusOK, &ShareDocumentResponse{
			GuestId: &guestId,
		})
		return
	}
}

// delete a user or guests permissions on a document
// (DELETE /document/{documentId}/permission/principal/{principalId})
func (s *Service) DeleteDocumentDocumentIdPermissionPrincipalPrincipalId(
	w http.ResponseWriter, 
	r *http.Request, 
	documentId DocumentId, 
	principalId PrincipalId,
) {
	// parse the claims and the principal id from the claims 
	claims, err := GetClaims(r.Context())
	if err != nil {
		SendError(w, http.StatusInternalServerError, "Internal server error")
		return
	}
	callingPrincipalId, err := claims.ParsePrincipalId()
	if err != nil {
		SendError(w, http.StatusBadRequest, err.Error())
		return
	}
	// perform a coarse grain authorization check, only user type tokens should be able to
	// delete permissions on a document
	if claims.GetTokenType() != PrincipalTypeUser {
		SendError(w, http.StatusForbidden, "only users type tokens can delete permissions")
		return
	}
	// call the document service to delete this permission
	err = s.documentServiceClient.DeletePermissionsPrincipal(
		r.Context(), principalId, documentId, callingPrincipalId,
	)
	if err != nil {
		SendError(w, GrpcToHttpStatus(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// get the permission of a principal on a document
// (GET /document/{documentId}/permission/principal/{principalId})
func (s *Service) GetDocumentDocumentIdPermissionPrincipalPrincipalId(
	w http.ResponseWriter, 
	r *http.Request, 
	documentId DocumentId, 
	principalId PrincipalId,
) {
	// parse the claims and the principal id from the claims 
	claims, err := GetClaims(r.Context())
	if err != nil {
		SendError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	callingPrincipalId, err := claims.ParsePrincipalId()
	if err != nil {
		SendError(w, http.StatusForbidden, err.Error())
		return
	}
	// coarse grain check, guests cannot get the permission of a principal on a document
	// unless they are the principal that they are checking 
	if claims.GetTokenType() == PrincipalTypeGuest && principalId != callingPrincipalId {
		SendError(w, http.StatusForbidden, "guests cannot get the permissions of other principals on documents")
		return
	}
	// call the document service to get the permission of the principal on this document
	result, err := s.documentServiceClient.GetPermissionsOfPrincipalOnDocument(
		r.Context(), documentId, principalId, callingPrincipalId,
	)
	if err != nil {
		SendError(w, GrpcToHttpStatus(err), err.Error())
		return
	}
	// reformat the returned permission so that it can be sent over http instead of gRPC
	permission, err := protoToNetPermission(result.Permission)
	if err != nil {
		SendError(w, http.StatusInternalServerError, "Internal server error")
		return
	}
	SendJsonResponse(
		w, http.StatusOK, permission,
	)
}

// update the permission level of a user or a guest on a document
// (PUT /document/{documentId}/permission/principal/{principalId})
func (s *Service) PutDocumentDocumentIdPermissionPrincipalPrincipalId(
	w http.ResponseWriter,
	r *http.Request, 
	documentId DocumentId, 
	principalId PrincipalId,
) {
	// parse the claims and the calling principal id from the JWT
	claims, err := GetClaims(r.Context())
	if err != nil {
		SendError(w, http.StatusInternalServerError, "Internal server error")
		return
	}
	callingPrincipalId, err := claims.ParsePrincipalId()
	if err != nil {
		SendError(w, http.StatusBadRequest, err.Error())
		return
	}
	// coarse grain check: only users can have the permission level of owner, hence only users can
	// update the permission of other users or guests on a document
	// the document service will do fine grain permission checks
	if claims.GetTokenType() == PrincipalTypeGuest {
		SendError(w, http.StatusForbidden, "guests cannot change the permissions of other principals")
		return
	}
	// parse the request body including the new permission level
	var reqBody PutDocumentDocumentIdPermissionPrincipalPrincipalIdJSONRequestBody
	err = json.NewDecoder(r.Body).Decode(&reqBody)
	if err != nil {
		SendError(w, http.StatusBadRequest, fmt.Sprintf(
			"failed to parse the request body with error: %v", err,
		))
		return
	}
	// translate the request permission level to a proto compatible permission level
	permissionLevel, err := netToProtoPermissionLevel(reqBody.PermissionLevel)
	if err != nil {
		SendError(w, http.StatusBadRequest, "invalid permission level")
		return
	}
	// call the document service 
	// if this is a user principal type then call the document service upsert permission
	// user rpc, if this is a guest principal type then call the update permission guest rpc
	if reqBody.PrincipalType == PrincipalTypeUser {
		err = s.documentServiceClient.UpsertPermissionUser(
			r.Context(), principalId, callingPrincipalId, documentId, permissionLevel,
		)
		if err != nil {
			SendError(w, GrpcToHttpStatus(err), err.Error())
			return
		} else {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	} else {
		err = s.documentServiceClient.UpdatePermissionGuest(
			r.Context(), principalId, callingPrincipalId, permissionLevel,
		)
		if err != nil {
			SendError(w, GrpcToHttpStatus(err), err.Error())
			return
		} else {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
}

/*
CHECKPOINT:
- you just finished implementing permissions routes
- next there is many bugfixes to implement
	- make sure that the routes that have permission checks are using the new permission enums
	- look into wether I should be using a pointer or value receiver for the route handler 
	  functions

*/