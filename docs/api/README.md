# Documentation for sandboxd

<a name="documentation-for-api-endpoints"></a>
## Documentation for API Endpoints

All URIs are relative to *http://127.0.0.1:8080*

| Class | Method | HTTP request | Description |
|------------ | ------------- | ------------- | -------------|
| *DefaultApi* | [**createSnapshot**](Apis/DefaultApi.md#createSnapshot) | **POST** /v1/snapshots |  |
*DefaultApi* | [**deleteImage**](Apis/DefaultApi.md#deleteImage) | **DELETE** /v1/images/{chain_id} |  |
*DefaultApi* | [**deleteSnapshot**](Apis/DefaultApi.md#deleteSnapshot) | **DELETE** /v1/snapshots/{chain_id} |  |
*DefaultApi* | [**health**](Apis/DefaultApi.md#health) | **GET** /healthz |  |
*DefaultApi* | [**listImages**](Apis/DefaultApi.md#listImages) | **GET** /v1/images |  |
*DefaultApi* | [**listSnapshots**](Apis/DefaultApi.md#listSnapshots) | **GET** /v1/snapshots |  |
*DefaultApi* | [**prepareImage**](Apis/DefaultApi.md#prepareImage) | **POST** /v1/images/prepare |  |
*DefaultApi* | [**run**](Apis/DefaultApi.md#run) | **POST** /v1/runs |  |


<a name="documentation-for-models"></a>
## Documentation for Models

 - [DeletedOutputBody](./Models/DeletedOutputBody.md)
 - [ErrorDetail](./Models/ErrorDetail.md)
 - [ErrorModel](./Models/ErrorModel.md)
 - [HealthOutputBody](./Models/HealthOutputBody.md)
 - [ImagesOutputBody](./Models/ImagesOutputBody.md)
 - [JobResult](./Models/JobResult.md)
 - [MachineConfig](./Models/MachineConfig.md)
 - [PrepareImageInputBody](./Models/PrepareImageInputBody.md)
 - [PreparedImage](./Models/PreparedImage.md)
 - [RunOutputBody](./Models/RunOutputBody.md)
 - [RunRequest](./Models/RunRequest.md)
 - [RunTimingsDTO](./Models/RunTimingsDTO.md)
 - [SnapshotArtifact](./Models/SnapshotArtifact.md)
 - [SnapshotInfo](./Models/SnapshotInfo.md)
 - [SnapshotInputBody](./Models/SnapshotInputBody.md)
 - [SnapshotsOutputBody](./Models/SnapshotsOutputBody.md)
 - [Task](./Models/Task.md)
 - [VMHandleDTO](./Models/VMHandleDTO.md)
 - [VMTimingsDTO](./Models/VMTimingsDTO.md)


<a name="documentation-for-authorization"></a>
## Documentation for Authorization

All endpoints do not require authorization.
