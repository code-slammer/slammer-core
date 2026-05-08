# Task
## Properties

| Name | Type | Description | Notes |
|------------ | ------------- | ------------- | -------------|
| **argv** | **List** | Argument vector for exec tasks. No implicit shell is added. | [optional] [default to null] |
| **contents\_b64** | **byte[]** | Base64-encoded file contents for write_file tasks. | [optional] [default to null] |
| **create\_parents** | **Boolean** | Create missing parent directories for mkdir or write_file tasks. | [optional] [default to false] |
| **mode** | **Integer** | Unix permission bits for mkdir or write_file. Defaults to 0755 for mkdir and 0644 for write_file. | [optional] [default to 420] |
| **path** | **String** | Guest path for mkdir and write_file tasks. Must be under /workspace. | [optional] [default to null] |
| **type** | **String** | Task type to run in the guest agent. | [default to null] |
| **working\_dir** | **String** | Guest working directory for this exec task. Must be under /workspace. | [optional] [default to /workspace] |

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)

