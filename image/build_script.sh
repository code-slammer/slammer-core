#!/bin/bash
set -e

ROOT_FS=/myfs
# rc-update add devfs boot
# rc-update add procfs boot
# rc-update add sysfs boot

# Make sure special file systems are mounted on boot:
for d in bin etc lib root sbin usr home; do tar c "/$d" | tar x -C $ROOT_FS; done

# The above command may trigger the following message:
# tar: Removing leading "/" from member names
# However, this is just a warning, so you should be able to
# proceed with the setup process.
for dir in dev proc run sys var; do mkdir -p $ROOT_FS/${dir}; done

mkdir -p $ROOT_FS/overlay/root \
    $ROOT_FS/overlay/work \
    $ROOT_FS/mnt \
    $ROOT_FS/rom

exit