# Description:
- add to the api gateway service an implementation of the document crud routes
- make sure that the routes are authenticated such that only users with the correct permissions can make modifications

# Technical requirements:
- [ ] implement the document crud routes:
    - [ ] create a new document
    - [ ] get all the documents that a user has owner permissions on
    - [ ] delete a list of documents
        - [ ] ensure that the route only works for documents that the user has owner permissions on
        - [ ] should delete atomically all the documents
    - [ ] get one document by document id 
        - [ ] ensure the user has permission to view this document
    - [ ] update one document by document id
        - [ ] ensure the user has permission to edit this document
    - [ ] delete one document by document id
        - [ ] ensure the user has owner permission on this document
    - [ ] return informative error messages if the user does not have the requisite permission on that document    
- 