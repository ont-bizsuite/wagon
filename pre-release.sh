set -ex

find . -name "*.go" | xargs sed -i 's/github.com\/ontio/github.com\/ont-bizsuite/g'
sed -i 's/github.com\/ontio/github.com\/ont-bizsuite/g' go.mod
