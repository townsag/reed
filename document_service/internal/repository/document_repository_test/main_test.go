package document_repository_test

/*
Ways of making mutations, permissions:
- CreateDocument, adds a permission to that document
- CreateGuest, adds a guest and a permission on a document
- UpsertPermissionsUser, creates a permission on a document or updates a permission on a document
- UpdatePermissionGuest, updates a permission on a document for a guest
- DeletePermissionsPrincipal, deletes the permission on a document for either a
Ways of observing those mutations:
- ListDocumentsByPrincipal, list the documents that a principal has permission to access, takes a list of permission levels as a parameter
- ListPermissionsOnDocument, list the principals and permission levels on that document
- GetPermissionOfPrincipalOnDocument, list the permissions of a principal on a document

Testing strategy:
- [ ] document crud tests:
	- [x] create document -> get document -> update document -> get document
	- [x] create document -> get document -> delete document -> get document
	- [ ] verify that deleting a document also deletes all the permissions on that document and the guests that had permissions on that document
- [ ] permission crud:
	- verify that when a permission is created all the fields are correctly populated
- [ ] guest crud testing:
	- [ ] verify that deleting a document deletes the guests that had permission on that document
	- [ ] verify that deleting a guests permission on a document also deletes the guest? should we do this... probably
- [ ] verify that that for each method of observing mutations on permissions, the methods for observing mutations can observe each type of mutation
	- [x] ListDocumentsByPrincipal flows:
		- [x] create document -> view document and permission in list by user id -> delete document -> view that the document is deleted / missing in the list by user id
		- [x] create document -> view document and permission in list by user id -> delete permissions of user on document -> view that the document is missing in the list by user id
		- [x] create document -> view document and permission in list by user id -> update permission -> view the updated permission in the list by user id
		- [x] create a document -> share the document with a guest -> view document in list by guest id -> delete document -> view that document is missing from list by guest id
		- [x] create a document -> share the document with a guest -> view document in the list by guest id -> update the permissions of the guest on document -> view change in permissions on document in list docs by guest id
		- [x] create a document -> share the document with a guest -> view document in list by guest id -> delete the permission of the guest on that document -> view that the permission is missing in list docs by guest id
		- [x] create a few documents -> share each with the same user -> verify that the permission filtering works by listing docs by user id with different permission level params
	- [x] ListPermissionsOnDocument flows:
		- [x] create a document -> share the document with a user -> delete the document -> verify that we get a not found error after listing permissions on a deleted document
			- [x] verify that the cursor returned for each call to the list permissions by document method are well formed
			- do we need to verify at the database level that there are no more permissions in that table on that document? Or is it enough that the permissions cannot be reached using the api provided by the document repo package
		- [x] create a document -> share the document with a user -> verify that the permissions are present for both using list by document -> delete the permissions on the shared user -> verify that the permissions are missing for the shared user
		- [x] create a document -> share the document with a user -> verify that the permissions are present for both using list by document -> update the permission on the shared user -> verify that the permissions are updated for the shared user
		- [x] create a document -> share the document with a guest -> verify that the permissions are present -> update the permissions of the guest -> verify that the permissions are updated using list permissions by doc
		- [x] create a document -> share the document with a guest -> verify that the permissions are present -> delete the guests permissions on that document -> verify that the permissions are removed by listing permissions by document
		- [x] create a document -> share the document with a few users at different permission levels -> verify that permission level filtering works by calling list permissions by document id
	- [x] GetPermissionOfPrincipalOnDocument:
		- [x] create a document -> share document with a user -> verify that the user has permissions on document -> update permission of user on document -> verify that the permission of the user has changed
		- [x] create a document -> share document with guest -> verify guest has permission on document -> update the permission of guest on doc -> verify that the permissions of the guest on doc have changed
		- [x] create a document -> share the document with user -> verify the user has permissions on doc -> delete the permissions of user on document -> verify that the permissions have been deleted
		- [x] create a document -> share the document with a guest -> verify the guest has permissions on doc -> delete the permissions of guest on doc -> verify that guest no longer has permissions on doc
- [ ] verify the failure not found cases for all the methods of making mutations and observing mutations
	- [ ] GetDocument:
		- [ ] calling get document on a document that doesn't exist returns an error
	- [ ] UpdateDocument:
		- [ ] calling update document on a document that doesn't exist returns an error
	- [ ] DeleteDocument:
		- [ ] calling delete document on a document that does not exist returns an error
	- [x] GetPermissionOfPrincipalOnDocument:
		- [x] calling get permissions of principal on doc with an invalid doc id returns a not found error
	- [x] ListPermissionsOnDocument
		- [x] calling list permissions on doc with a missing doc id returns a not found error
	- [ ] CreateGuest
		- [ ] calling create guest with a document id that does not exist returns a not found error
	- [ ] UpsertPermissionsUser
		- [ ] calling upsert permission user on a document that does not exist returns an error
	- [ ] UpdatePermissionGuest
		- [ ] calling update permissions guest on a guest that does not exist returns a not found error
		- [ ] calling update permissions guest on a document that does not exist returns a not found error
	- [ ] DeletePermissionsPrincipal
		- [ ] calling delete permissions principal on a combination of principal and document that does not exist returns a not found error
- [ ] input validation checks:
	- [ ] UpdateDocument:
		- [ ] calling update document with no non-nil inputs returns an error
	- [ ] ListDocumentsByPrincipal
		- [x] calling list docs by principal with an invalid cursor returns an error
		- [x] calling list docs by principal with an invalid permission set returns an error
			- [x] empty list
			- [x] list with invalid permission in it
	- [x] ListPermissionsOnDocument
		- [x] calling list permissions on principal with with an invalid cursor returns an error
		- [x] calling list permissions on principal with an invalid permission set returns an error
			- [x] empty list
			- [x] list with invalid permission in it
	- [ ] CreateGuest
		- [ ] calling create guest with an invalid permission returns an error
		- [ ] calling create guest with owner permissions returns an error
	- [ ] UpsertPermissionsUser
		- [ ] calling upsert permissions user with an invalid permission returns an error
		- [ ] calling upsert permissions user the owner permission level on a document that already has an owner results in an error
	- [ ] UpdatePermissionGuest
		- [ ] calling update permissions guest with an invalid permission returns an error
		- [ ] calling update permissions guest with owner permissions returns an error
- [ ] cursor based pagination implementation tests:
	- [ ] linearly traverse, created by sorting:
		- [ ] create a few documents -> share those documents with a user -> verify that the pagination logic works by listing those documents over a multiple pages
		- [ ] the documents should be ordered by created by in reverse chronological order
	- [ ] linear traverse, last updated by sorting
		- [ ] create a few documents -> share those documents with a user -> update one of the documents -> verify that the sorting logic works by listing those documents by last modified
	- [ ] linear traverse, cursor pagination logic with inserts after cursor
		- [ ] create a few documents -> share those documents with a user -> list the documents and save cursor -> create a few new documents -> traverse the rest of the shared documents and verify that the new documents are not listed
	- [ ] the cursor indicates that the last item has been found when necessary
	- [ ] if there are no documents associated with a cursor, the returned cursor should be the nil cursor
- test repo implementation helper functions:



TODO: verify that tests make sure that deleting a document deletes the permissions on that document
TODO: verify that an owner of a document cannot change their permissions on that document
TODO: verify that only users / guests with owner permissions can modify the permissions on a document. Should this happen at the repo level or at the service level? Will making this check at the repo level mean that we don't have to add complicated transaction logic to the service level?
*/

/*
## Notes on naming:
- functions that conform to the format Test...Integration use testcontainers to access a postgres database instance
	- this allows us to conditionally run tests that require a testcontainers instance using regex
- functions that conform to the format Test...Unit use a simple in memory mock instead of using testcontainers
	- these run much faster but often only validate the null checking and other argument validation logic

## Notes on testing best practices:
- I have heard that each test should test exactly one thing. I think this is impractical and will lead to too many tests and repeated work
- instead, this test suite mostly tests sequential flows of operations and validates that the result of those sequential flows of operations is correct
	- I think this results in tests that are verbose but few, also it results in tests that closely mirror the way that the repository will be used by the calling code
*/

import (
	"testing"
	"os"
)

func TestMain(m *testing.M) {
	// run the tests
	code := m.Run()
	// now that the tests have been run, cleanup the postgres container
	cleanupPostgresContainer()
	os.Exit(code)
}