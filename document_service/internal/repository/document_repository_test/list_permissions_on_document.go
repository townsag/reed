package document_repository

import (
	"testing"
)

func TestListPermissionsOnDocument_OnUser_DeletePermissionPath_Integration(t *testing.T) {
	// create a dummy user
	// create a document
	// create a dummy recipient user to share that document with
	// share the document with the recipient 
	// verify that the user and the recipient both have permissions on the document
	// delete the recipients permissions on that document
	// verify that now only the user has permissions on the document
}