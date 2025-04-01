package rpc

import "os"

type UploadFileArgs struct {
	FilePath    string // Path to the file to be uploaded
	Permissions uint32 // File permissions (Unix-style)
	// e.g., 0644 for rw-r--r--
	UID      int    // User ID for the file owner
	GID      int    // Group ID for the file owner
	Contents []byte // File contents
}

type UploadFileReply struct{}

// upload a file to the VM
func (*VMService) UploadFile(args UploadFileArgs, reply *UploadFileReply) error {
	err := os.WriteFile(args.FilePath, args.Contents, os.FileMode(args.Permissions))
	if err != nil {
		return err
	}
	// Set the file owner
	err = os.Chown(args.FilePath, args.UID, args.GID)
	if err != nil {
		return err
	}
	return nil
}
