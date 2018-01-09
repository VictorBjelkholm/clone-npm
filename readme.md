- Got new instance
  - Make sure security group for port 5984 is open
- Run `echo "deb https://apache.bintray.com/couchdb-deb xenial main" | sudo tee -a /etc/apt/sources.list`
- Run `curl -L https://couchdb.apache.org/repo/bintray-pubkey.asc | sudo apt-key add -`
- Run `sudo apt update`
- Run `sudo apt install couchdb`
- Change config to bind public IP
- Go to 54.183.196.81:5984/_utils/
- Setup replication
  - http://54.183.196.81:5984/_utils/#/replication/_create
  - http://skimdb.npmjs.com/registry
- Create new view
  - http://54.183.196.81:5984/_utils/#/database/npm-registry/new_view
  - _design/main
  - index name: all-versions
```
function (doc) {
  var versions = {}
  Object.keys(doc.versions).forEach(function(version) {
    versions[version] = doc.versions[version].dist
  })
  emit(doc._id, versions)
}
```

```
curl -H "Content-Type: application/json" "http://54.183.196.81:5984/npm-registry/_design/main/_view/all-versions?stale=ok&limit=1&skip=0" | jq .
```

