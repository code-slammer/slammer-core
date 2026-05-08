# DefaultApi

All URIs are relative to *http://127.0.0.1:8080*

| Method | HTTP request | Description |
|------------- | ------------- | -------------|
| [**createSnapshot**](DefaultApi.md#createSnapshot) | **POST** /v1/snapshots |  |
| [**deleteImage**](DefaultApi.md#deleteImage) | **DELETE** /v1/images/{chain_id} |  |
| [**deleteSnapshot**](DefaultApi.md#deleteSnapshot) | **DELETE** /v1/snapshots/{chain_id} |  |
| [**health**](DefaultApi.md#health) | **GET** /healthz |  |
| [**listImages**](DefaultApi.md#listImages) | **GET** /v1/images |  |
| [**listSnapshots**](DefaultApi.md#listSnapshots) | **GET** /v1/snapshots |  |
| [**prepareImage**](DefaultApi.md#prepareImage) | **POST** /v1/images/prepare |  |
| [**run**](DefaultApi.md#run) | **POST** /v1/runs |  |


<a name="createSnapshot"></a>
# **createSnapshot**
> SnapshotArtifact createSnapshot(SnapshotInputBody)



### Parameters

|Name | Type | Description  | Notes |
|------------- | ------------- | ------------- | -------------|
| **SnapshotInputBody** | [**SnapshotInputBody**](../Models/SnapshotInputBody.md)|  | |

### Return type

[**SnapshotArtifact**](../Models/SnapshotArtifact.md)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: application/json
- **Accept**: application/json, application/problem+json

<a name="deleteImage"></a>
# **deleteImage**
> DeletedOutputBody deleteImage(chain\_id)



### Parameters

|Name | Type | Description  | Notes |
|------------- | ------------- | ------------- | -------------|
| **chain\_id** | **String**| Rootfs chain ID. The sha256: prefix is accepted but not required. | [default to null] |

### Return type

[**DeletedOutputBody**](../Models/DeletedOutputBody.md)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: Not defined
- **Accept**: application/json, application/problem+json

<a name="deleteSnapshot"></a>
# **deleteSnapshot**
> DeletedOutputBody deleteSnapshot(chain\_id)



### Parameters

|Name | Type | Description  | Notes |
|------------- | ------------- | ------------- | -------------|
| **chain\_id** | **String**| Rootfs chain ID. The sha256: prefix is accepted but not required. | [default to null] |

### Return type

[**DeletedOutputBody**](../Models/DeletedOutputBody.md)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: Not defined
- **Accept**: application/json, application/problem+json

<a name="health"></a>
# **health**
> HealthOutputBody health()



### Parameters
This endpoint does not need any parameter.

### Return type

[**HealthOutputBody**](../Models/HealthOutputBody.md)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: Not defined
- **Accept**: application/json, application/problem+json

<a name="listImages"></a>
# **listImages**
> ImagesOutputBody listImages()



### Parameters
This endpoint does not need any parameter.

### Return type

[**ImagesOutputBody**](../Models/ImagesOutputBody.md)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: Not defined
- **Accept**: application/json, application/problem+json

<a name="listSnapshots"></a>
# **listSnapshots**
> SnapshotsOutputBody listSnapshots()



### Parameters
This endpoint does not need any parameter.

### Return type

[**SnapshotsOutputBody**](../Models/SnapshotsOutputBody.md)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: Not defined
- **Accept**: application/json, application/problem+json

<a name="prepareImage"></a>
# **prepareImage**
> PreparedImage prepareImage(PrepareImageInputBody)



### Parameters

|Name | Type | Description  | Notes |
|------------- | ------------- | ------------- | -------------|
| **PrepareImageInputBody** | [**PrepareImageInputBody**](../Models/PrepareImageInputBody.md)|  | |

### Return type

[**PreparedImage**](../Models/PreparedImage.md)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: application/json
- **Accept**: application/json, application/problem+json

<a name="run"></a>
# **run**
> RunOutputBody run(RunRequest)



### Parameters

|Name | Type | Description  | Notes |
|------------- | ------------- | ------------- | -------------|
| **RunRequest** | [**RunRequest**](../Models/RunRequest.md)|  | |

### Return type

[**RunOutputBody**](../Models/RunOutputBody.md)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: application/json
- **Accept**: application/json, application/problem+json

