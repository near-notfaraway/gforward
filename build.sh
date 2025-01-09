CURRENT_DIR=$(cd $(dirname $0); pwd)
BUILD_DIR=$CURRENT_DIR/output
PROJECT_NAME='gforward'

cd $CURRENT_DIR
rm -rf $BUILD_DIR

mkdir -p $BUILD_DIR/$PROJECT_NAME/bin
mkdir -p $BUILD_DIR/$PROJECT_NAME/config
mkdir -p $BUILD_DIR/$PROJECT_NAME/logs

if [[ "$SYSTEM" == "darwin" ]]; then
    CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o $BUILD_DIR/$PROJECT_NAME/bin/$PROJECT_NAME
elif [[ "$SYSTEM" == "linux" ]]; then
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $BUILD_DIR/$PROJECT_NAME/bin/$PROJECT_NAME
else
    CGO_ENABLED=0 go build -o $BUILD_DIR/$PROJECT_NAME/bin/$PROJECT_NAME
fi

cd $BUILD_DIR
tar -czf ./$PROJECT_NAME.tgz ./$PROJECT_NAME
#rm -rf ./$PROJECT_NAME
