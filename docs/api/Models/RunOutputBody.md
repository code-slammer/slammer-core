# RunOutputBody
## Properties

| Name | Type | Description | Notes |
|------------ | ------------- | ------------- | -------------|
| **$schema** | **URI** | A URL to the JSON Schema for this object. | [optional] [default to null] |
| **image** | [**PreparedImage**](PreparedImage.md) | Prepared image used for this run. | [default to null] |
| **results** | [**List**](JobResult.md) | Guest agent job results in task order, including the automatically prepended mkdir. | [default to null] |
| **timings** | [**RunTimingsDTO**](RunTimingsDTO.md) | High-level manager timing breakdown in milliseconds. | [default to null] |
| **vm** | [**VMHandleDTO**](VMHandleDTO.md) | Low-level VM handle metadata for this completed run. | [default to null] |
| **vm\_timings** | [**VMTimingsDTO**](VMTimingsDTO.md) | Firecracker and agent timing breakdown in milliseconds. | [default to null] |

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)

