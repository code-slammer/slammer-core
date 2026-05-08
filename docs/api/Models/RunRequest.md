# RunRequest
## Properties

| Name | Type | Description | Notes |
|------------ | ------------- | ------------- | -------------|
| **$schema** | **URI** | A URL to the JSON Schema for this object. | [optional] [default to null] |
| **gid** | **Long** | Guest GID used by task defaults when a task does not override it. | [optional] [default to 0] |
| **image\_ref** | **String** | OCI image reference to run. The daemon prepares the image if needed. | [default to null] |
| **machine** | [**MachineConfig**](MachineConfig.md) | Optional per-run machine sizing. Values are capped by daemon config. | [optional] [default to null] |
| **snapshot\_mode** | **String** | Snapshot behavior for the run. disabled cold-boots; auto restores or creates the configured chain snapshot. | [optional] [default to disabled] |
| **tasks** | [**List**](Task.md) | Ordered guest tasks to execute. A /workspace mkdir task is automatically prepended. | [default to null] |
| **timeout\_millis** | **Long** | Maximum wall-clock time for the VM run and task defaults. Capped by daemon config. | [optional] [default to 60000] |
| **uid** | **Long** | Guest UID used by task defaults when a task does not override it. | [optional] [default to 0] |
| **workdir** | **String** | Default guest working directory for exec tasks. Must be under /workspace. | [optional] [default to /workspace] |

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)

