# Description:
- add to the api gateway service an implementation of the document crud routes
- make sure that the routes are authenticated such that only users with the correct permissions can make modifications

# Technical requirements:
- [ ] implement the document crud routes:
    - [x] create a new document
    - [x] get all the documents that a user has owner permissions on
    - [x] delete a list of documents
        - [ ] ensure that the route only works for documents that the user has owner permissions on
        - [x] should delete atomically all the documents
    - [x] get one document by document id 
        - [ ] ensure the user has permission to view this document
    - [x] update one document by document id
        - [ ] ensure the user has permission to edit this document
    - [x] delete one document by document id
        - [ ] ensure the user has owner permission on this document
    - [ ] return informative error messages if the user does not have the requisite permission on that document
    - ^ decide to pass calling principal id down to the document service and let the document service handle all the permission logic
- [x] implement coarse grain authorization at the api gateway level:
    - creating and deleting routes can only be done by users, not guests
    - updating documents can be done by guests
    - if we can get rid of these broad strokes rules in the api-gateway layer then we can save ourself some headache
    - this means that for some of the routes we will have to validate the token type so that we can guarantee that the principal id passed to the userId argument of the document service client function actually is a user id and not a guest id
- [x] code quality checks
    - [x] cleanup the principal id parsing code such that one sentinel error is used and I don't type out 15 different status bad request server response strings 