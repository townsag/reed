package server

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	emptypb "google.golang.org/protobuf/types/known/emptypb"
	"github.com/google/uuid"

	pb "github.com/townsag/reed/document_service/api/v1"
	"github.com/townsag/reed/document_service/internal/service"
)

type DocumentServiceServerImpl struct {
	pb.UnimplementedDocumentServiceServer
	documentService *service.DocumentService
}

var _ pb.DocumentServiceServer = (*DocumentServiceServerImpl)(nil)

func NewDocumentServiceImpl(documentService *service.DocumentService) *DocumentServiceServerImpl {
	return &DocumentServiceServerImpl{
		documentService: documentService,
	}
}

/*
## What is this layer for?
- the server layer if for:
	- everything to do with the wire protocol
		- serializing and deserializing input
		- sending error messages
	- cross cutting concerns that need to be handled for all endpoints
		- authentication
		- observability:
			- logging
			- tracing
			- metrics

- each function in this layer should look something like this:
	- perform runtime checks to ensure that the required fields are there
	- translate the gPRC input into service structs
	- call the necessary function on the document service
	- if necessary, return an error response wrapped in gPRC error
	- translate the service response to protobuf response
	- return the protobuf response
*/

func serviceToGRPCError(err error) error {
	// something like this could be used to determine if the error is one of our wrapped domain errors or 
	// if it is a unknown error type
	// var domainError *service.DomainError
	// errors.As(error, &domainError)
	var notFound *service.NotFoundError
	var uniqueError *service.UniqueConflictError
	var invalidError *service.InvalidInputError

	switch {
	case err == nil:
		return nil
	case errors.As(err, &notFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.As(err, &uniqueError):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.As(err, &invalidError):
		return status.Error(codes.InvalidArgument, err.Error())
	// the repo implementation error falls into the default case of internal server error
	default:
		return status.Error(codes.Internal, "internal server error encountered")
	}
}

func pbToServicePermissionLevel(permissionLevel pb.PermissionLevel) (service.PermissionLevel, error) {
	switch permissionLevel {
	case pb.PermissionLevel_PERMISSION_VIEWER:
		return service.Viewer, nil
	case pb.PermissionLevel_PERMISSION_EDITOR:
		return service.Editor, nil
	case pb.PermissionLevel_PERMISSION_OWNER:
		return service.Owner, nil
	default:
		return -1, fmt.Errorf("failed to match any valid service permission levels for permission: %v", permissionLevel)
	}
}

func pbToServicePermissionLevelList(
	permissions []pb.PermissionLevel,
) ([]service.PermissionLevel, error) {
	// the overhead of allocating a new slice for each request is not significant when compared to the
	// rtt of the database call etc. This should be optimized if we can show that it is a bottleneck
	// but that is unlikely because these are small slices of integers
	servicePermissions := make([]service.PermissionLevel, 0, len(permissions))
	for _, pbPermission := range permissions {
		servicePermission, err := pbToServicePermissionLevel(pbPermission)
		if err != nil {
			return nil, err
		}
		servicePermissions = append(servicePermissions, servicePermission)
	}
	return servicePermissions, nil
}

func serviceToPbPermissionLevel(
	permissionLevel service.PermissionLevel,
) (pb.PermissionLevel, error) {
	switch permissionLevel {
	case service.Viewer:
		return pb.PermissionLevel_PERMISSION_VIEWER, nil
	case service.Editor:
		return pb.PermissionLevel_PERMISSION_EDITOR, nil
	case service.Owner:
		return  pb.PermissionLevel_PERMISSION_OWNER, nil
	default:
		return -1, fmt.Errorf("failed to map a valid pb permission level to: %v", permissionLevel)
	}
}

func serviceToPbDocument(document service.Document) (*pb.Document) {
	return &pb.Document{
		DocumentId: document.ID.String(),
		DocumentName: document.Name,
		Description: document.Description,
		CreatedAt: timestamppb.New(document.CreatedAt),
		LastModifiedAt: timestamppb.New(document.LastModifiedAt),
	}
}

func serviceToPbDocumentPermissionList(
	documentPermissions []service.DocumentPermission,
) ([]*pb.ListDocumentsByPrincipalReply_DocumentPermission, error) {
	result := make([]*pb.ListDocumentsByPrincipalReply_DocumentPermission, len(documentPermissions))
	for i, elem := range documentPermissions {
		// serialize the service permission to a pb permission
		document := serviceToPbDocument(elem.Document)
		// serialize the service document to a pb document
		permissionLevel, err := serviceToPbPermissionLevel(elem.Permission)
		if err != nil {
			return nil, err
		}
		// add the constructed object to the result list
		result[i] = &pb.ListDocumentsByPrincipalReply_DocumentPermission{
			Document: document,
			PermissionLevel: permissionLevel,
		}
	}
	return result, nil
}

func pbToServiceSortField(
	sortField pb.Cursor_SortField,
) (service.SortField, error) {
	switch sortField {
	case pb.Cursor_SORT_FIELD_CREATED_AT:
		return service.CreatedAt, nil
	case pb.Cursor_SORT_FIELD_LAST_MODIFIED_AT:
		return service.LastModifiedAt, nil
	default:
		return -1, fmt.Errorf("failed to match any valid service sort fields for sort field: %v", sortField)
	}
}

func serviceToPbSortField(
	sortField service.SortField,
) (pb.Cursor_SortField, error) {
	switch sortField {
	case service.CreatedAt:
		return pb.Cursor_SORT_FIELD_CREATED_AT, nil
	case service.LastModifiedAt:
		return pb.Cursor_SORT_FIELD_LAST_MODIFIED_AT, nil
	default:
		return -1, fmt.Errorf("failed to find a valid pb sort field for: %v", sortField)
	}
}

func serviceToPbCursor(cursor service.Cursor) (*pb.Cursor, error) {
	sortField, err := serviceToPbSortField(cursor.SortField)
	temp := cursor.LastSeenID.String()
	if err != nil {
		return nil, err
	}
	return &pb.Cursor{
		SortField: sortField,
		LastSeenTime: timestamppb.New(cursor.LastSeenTime),
		LastSeenDocumentId: &temp,
	}, nil
}

type RequestWithCursor interface {
	GetCursor() *pb.Cursor
}

func parseServiceCursor(
	reqCursor *pb.Cursor,
) (*service.Cursor, error) {
	sortField, err := pbToServiceSortField(reqCursor.SortField)
	if err != nil {
		return nil, err
	}
	// if both last seen value and last seen id is missing, make a default cursor
	if reqCursor.LastSeenTime == nil {
		return service.NewBeginningCursor(sortField), nil
	} else {
		// conditionally parse the last seen id if it is not nil
		var docId uuid.UUID
		if reqCursor.LastSeenDocumentId != nil {
			docId, err = uuid.Parse(*reqCursor.LastSeenDocumentId)
			if err != nil {
				return nil, fmt.Errorf(
					"failed to parse documentId as a uuid: %v",
					*reqCursor.LastSeenDocumentId,
				)
			}
		} else {
			docId = service.MaxDocumentID()
		}
		return &service.Cursor{
			SortField: sortField,
			LastSeenTime: reqCursor.LastSeenTime.AsTime(),
			LastSeenID: docId,
		}, nil
	}
}

func serviceToPbRecipientType(
	recipientType service.RecipientType,
) (pb.Principal_PrincipalType, error) {
	switch recipientType {
	case service.Guest:
		return pb.Principal_GUEST, nil
	case service.User:
		return pb.Principal_USER, nil
	default:
		return -1, fmt.Errorf("failed to map service recipient type to pb principal type: %v", recipientType)
	}
}

func serviceToPbPermission(permission service.Permission) (*pb.Permission, error) {
	principalType, err := serviceToPbRecipientType(permission.RecipientType)
	if err != nil {
		return nil, fmt.Errorf("error encountered when serializing permission: %w", err)
	}
	permissionLevel, err := serviceToPbPermissionLevel(permission.PermissionLevel)
	if err != nil {
		return nil, fmt.Errorf("error encountered when serializing permission: %w", err)
	}
	return &pb.Permission{
		Recipient: &pb.Principal{
			PrincipalId: permission.RecipientID.String(),
			PrincipalType: principalType,
		},
		DocumentId: permission.DocumentID.String(),
		PermissionLevel: permissionLevel,
		CreatedBy: permission.CreatedBy.String(),
		CreatedAt: timestamppb.New(permission.CreatedAt),
		LastModifiedAt: timestamppb.New(permission.LastModifiedAt),
	}, nil
}

func serviceToPbPermissionList(recipientPermissions []service.Permission) ([]*pb.Permission, error) {
	result := make([]*pb.Permission, len(recipientPermissions))
	for i, elem := range recipientPermissions {
		pbPermission, err := serviceToPbPermission(elem)
		if err != nil {
			return nil, err
		}
		result[i] = pbPermission
	}
	return result, nil
}

func (s *DocumentServiceServerImpl) CreateDocument(
	ctx context.Context,
	createDocReq *pb.CreateDocumentRequest,
) (*pb.CreateDocumentReply, error) {
	// translate the string userId into a uuid
	userId, err := uuid.Parse(createDocReq.OwnerUserId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unable to parse owner user Id as uuid")
	}
	// call the service function with the validated inputs
	documentId, err := s.documentService.CreateDocument(
		ctx, userId, createDocReq.DocumentName, createDocReq.DocumentDescription,
	)
	// if necessary, translate the error to a grpc error
	if err != nil {
		return nil, serviceToGRPCError(err)
	}
	// translate the service response to protobuf response
	return &pb.CreateDocumentReply{
		DocumentId: documentId.String(),
	}, nil
}

func (s *DocumentServiceServerImpl) GetDocument(
	ctx context.Context,
	getDocReq *pb.GetDocumentRequest,
) (*pb.GetDocumentReply, error) {
	// translate the string userId into a uuid
	documentId, err := uuid.Parse(getDocReq.DocumentId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unable to parse documentId as uuid")
	}
	document, err := s.documentService.GetDocument(
		ctx, documentId,
	)
	if err != nil {
		return nil, serviceToGRPCError(err)
	}
	return &pb.GetDocumentReply{
		Document: &pb.Document{
			DocumentId: document.ID.String(),
			DocumentName: document.Name,
			Description: document.Description,
			CreatedAt: timestamppb.New(document.CreatedAt),
			LastModifiedAt: timestamppb.New(document.LastModifiedAt),
		},
	}, nil
}

func (s *DocumentServiceServerImpl) UpdateDocument(
	ctx context.Context,
	updateDocReq *pb.UpdateDocumentRequest,
) (*emptypb.Empty, error) {
	// check that at least one of name and description are not nil in the service
	// layer instead of here because that is a business logic check
	// translate the documentId to a uuid
	documentId, err := uuid.Parse(updateDocReq.DocumentId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to parse document id as uuid")
	}
	// translate the userId to a uuid
	// userId, err := uuid.Parse(updateDocReq.UserId)
	// if err != nil {
	// 	return nil, status.Errorf(codes.InvalidArgument, "failed to parse userId as uuid")
	// }
	// TODO: use the userId to verify that this user has update permissions on this document
	// call the update document service function
	err = s.documentService.UpdateDocument(
		ctx, documentId, updateDocReq.Name, updateDocReq.Description,
	)
	// return any errors if necessary
	if err != nil {
		return nil, serviceToGRPCError(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *DocumentServiceServerImpl) DeleteDocument(
	ctx context.Context,
	deleteDocReq *pb.DeleteDocumentRequest,
) (*emptypb.Empty, error) {
	// parse the documentID
	documentId, err := uuid.Parse(deleteDocReq.DocumentId)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "failed to parse documentId as uuid")
	}
	// TODO: parse the userId
	// TODO: validate that this user has owner permissions to delete this document
	// call the delete document service method
	err = s.documentService.DeleteDocument(ctx, documentId)
	// return any errors if necessary
	if err != nil {
		return nil, serviceToGRPCError(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *DocumentServiceServerImpl) DeleteDocuments(
	ctx context.Context,
	deleteDocsReq *pb.DeleteDocumentsRequest,
) (*emptypb.Empty, error) {
	// parse the document ids
	parsedDocumentIds := make([]uuid.UUID, len(deleteDocsReq.DocumentIds))
	for i, documentId := range deleteDocsReq.DocumentIds {
		parsedId, err := uuid.Parse(documentId)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "failed to parse document id: %s", documentId)
		}
		parsedDocumentIds[i] = parsedId
	}
	// parse the user id
	parsedUserId, err := uuid.Parse(deleteDocsReq.UserId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to parse user id: %s", deleteDocsReq.UserId)
	}
	// validate that the user has ownership permissions over each of the documents in the list 
	// call the delete documents service method
	err = s.documentService.DeleteDocuments(ctx, parsedDocumentIds, parsedUserId)
	if err != nil {
		return nil, serviceToGRPCError(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *DocumentServiceServerImpl) ListDocumentsByPrincipal(
	ctx context.Context,
	listDocReq *pb.ListDocumentByPrincipalRequest,
) (*pb.ListDocumentsByPrincipalReply, error) {
	// parse the principal id
	principalId, err := uuid.Parse(listDocReq.PrincipalId)
	if err != nil {
		return nil, status.Errorf(
			codes.InvalidArgument,
			"unable to parse documentId: %s as uuid",
			listDocReq.PrincipalId,
		)
	}
	// parse the permissions list
	permissionFilter, err := pbToServicePermissionLevelList(listDocReq.PermissionsFilter)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	// construct the cursor
	// parse the sort field
	cursor, err := parseServiceCursor(listDocReq.Cursor)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	// parse the page size
	var pageSize int32
	if listDocReq.PageSize == nil {
		pageSize = service.DefaultPageSize
	} else {
		pageSize = *listDocReq.PageSize
	}
	// call the relevant helper function
	documentPermissions, responseCursor, err := s.documentService.ListDocumentsByPrincipal(
		ctx, principalId, permissionFilter, cursor, pageSize,
	)
	// return any errors if necessary
	if err != nil {
		return nil, serviceToGRPCError(err)
	}
	// serialize list of documents and return cursor to a protobuf response
	pbDocumentPermissions, err := serviceToPbDocumentPermissionList(documentPermissions)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	// serialize the response cursor
	pbRespCursor, err := serviceToPbCursor(*responseCursor)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.ListDocumentsByPrincipalReply{
		DocumentPermissions: pbDocumentPermissions,
		Cursor: pbRespCursor,
	}, nil
}

func (s *DocumentServiceServerImpl) GetPermissionsOfPrincipalOnDocument(
	ctx context.Context,
	req *pb.GetPermissionsRequest,
) (*pb.GetPermissionsReply, error) {
	// parse the documentID as a uuid
	documentId, err := uuid.Parse(req.DocumentId)
	if err != nil {
		return nil, status.Errorf(
			codes.InvalidArgument, "unable to parse document ID as a uuid: %v", req.DocumentId,
		)
	}
	// parse the principalID as a uuid
	principalId, err := uuid.Parse(req.PrincipalId)
	if err != nil {
		return nil, status.Errorf(
			codes.InvalidArgument, "unable to parse principalId as a uuid: %v", req.PrincipalId,
		)
	}
	permission, err := s.documentService.GetPermissionOfPrincipalOnDocument(ctx, documentId, principalId)
	// return any error that I may have found
	if err != nil {
		return nil, serviceToGRPCError(err)
	}
	// serialize the permission object and return it
	pbPermission, err := serviceToPbPermission(permission)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.GetPermissionsReply{
		Permission: pbPermission,
	}, nil
}

func (s *DocumentServiceServerImpl) ListPermissionsOnDocument(
	ctx context.Context, 
	req *pb.ListPermissionsOnDocumentRequest,
) (*pb.ListPermissionsOnDocumentReply, error) {
	// parse the documentID
	documentId, err := uuid.Parse(req.DocumentId)
	if err != nil {
		return nil, status.Errorf(
			codes.InvalidArgument,
			"failed to parse documentID as a uuid: %v",
			req.DocumentId,
		)
	}
	// parse the list of permission level filters
	permissionFilter, err := pbToServicePermissionLevelList(req.PermissionsFilter)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	// construct the cursor
	// parse the sort field
	cursor, err := parseServiceCursor(req.Cursor)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	// optionally apply the default page size
	var pageSize int32
	if req.PageSize == nil {
		pageSize = service.DefaultPageSize
	} else {
		pageSize = *req.PageSize
	}
	recipientPermissions, respCursor, err := s.documentService.ListPermissionsOnDocument(
		ctx,
		documentId,
		permissionFilter,
		cursor,
		pageSize,
	)
	// conditionally return an error
	if err != nil {
		return nil, serviceToGRPCError(err)
	}
	// serialize the list of recipient permissions to pb
	pbRecipientPermissions, err := serviceToPbPermissionList(recipientPermissions)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	// serialize the response cursor to pb
	pbRespCursor, err := serviceToPbCursor(*respCursor)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	// return the serialized payload
	return &pb.ListPermissionsOnDocumentReply{
		RecipientPermissions: pbRecipientPermissions,
		Cursor: pbRespCursor,
	}, nil
}

func (s *DocumentServiceServerImpl) CreateGuest(
	ctx context.Context,
	req *pb.CreateGuestRequest,
) (*pb.CreateGuestReply, error) {
	// parse the documentID
	documentId, err := uuid.Parse(req.DocumentId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to parse document id as uuid: %v", req.DocumentId)
	}
	// parse the userId
	userId, err := uuid.Parse(req.UserId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to parse user Id as uuid: %v", req.UserId)
	}
	// parse the permission level
	permissionLevel, err := pbToServicePermissionLevel(req.PermissionLevel)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	// call the relevant service function
	guestId, err := s.documentService.CreateGuest(ctx, userId, documentId, permissionLevel)
	// return any error
	if err != nil {
		return nil, serviceToGRPCError(err)
	}
	// return the generated guest id
	return &pb.CreateGuestReply{
		GuestId: guestId.String(),
	}, nil
}

func (s *DocumentServiceServerImpl) UpsertPermissionUser(
	ctx context.Context,
	req *pb.UpsertPermissionUserRequest,
) (*emptypb.Empty, error) {
	// parse the user Id
	userId, err := uuid.Parse(req.UserId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to parse user Id as uuid: %v", req.UserId)
	}
	// parse the document Id
	documentId, err := uuid.Parse(req.DocumentId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to parse document id as uuid: %v", req.DocumentId)
	}
	// parse the permission level
	permissionLevel, err := pbToServicePermissionLevel(req.PermissionLevel)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	// call the relevant service function
	err = s.documentService.UpsertPermissionUser(
		ctx, userId, documentId, permissionLevel,
	)
	// return any relevant errors
	if err != nil {
		return nil, serviceToGRPCError(err)
	}
	// return empty proto
	return &emptypb.Empty{}, nil
}

func (s *DocumentServiceServerImpl) UpdatePermissionGuest(
	ctx context.Context,
	req *pb.UpdatePermissionGuestRequest,
) (*emptypb.Empty, error) {
	// parse the guestId
	guestId, err := uuid.Parse(req.GuestId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to parse guestId as uuid: %v", req.GuestId)
	}
	// parse the permission level
	permissionLevel, err := pbToServicePermissionLevel(req.PermissionLevel)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	// call the relevant service layer functions
	err = s.documentService.UpdatePermissionGuest(
		ctx, guestId, permissionLevel,
	)
	if err != nil {
		return nil, serviceToGRPCError(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *DocumentServiceServerImpl) DeletePermissionsPrincipal(
	ctx context.Context,
	req *pb.DeletePermissionsPrincipalRequest,
) (*emptypb.Empty, error) {
	// parse the recipient id
	recipientId, err := uuid.Parse(req.PrincipalId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to parse recipient id as uuid: %v", req.PrincipalId)
	}
	// parse the document id
	documentId, err := uuid.Parse(req.DocumentId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to parse document id as uuid: %v", req.DocumentId)
	}
	// call the relevant service level helper function
	err = s.documentService.DeletePermissionPrincipal(ctx, recipientId, documentId)
	// return any errors if necessary
	if err != nil {
		return nil, serviceToGRPCError(err)
	}
	// return an empty response
	return &emptypb.Empty{}, nil
}