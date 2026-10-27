#!/bin/sh
# Starts the full-system test's virtual machine (made by build-vm.sh) and
# reports its result:
#
#   test/system/run-vm.sh VM_DIR
#
# The machine runs the test by itself at boot, prints the result on its
# console and turns itself off (see units/). Everything it prints is saved
# in VM_DIR/console.log. The exit status is 0 only if the test passed.
set -eu

vm="${1:?usage: run-vm.sh VM_DIR}"
log="$vm/console.log"

# KVM makes the machine about ten times faster; without it (on a computer
# without hardware virtualization) it still works, slowly.
accel=tcg
if [ -w /dev/kvm ]; then
	accel=kvm
fi
echo "starting the virtual machine ($accel), console in $log"

# No network card: the test's networks are all simulated inside.
# -no-reboot ends QEMU when the machine turns itself off.
timeout 40m qemu-system-x86_64 -accel "$accel" -m 3G -smp 2 \
	-kernel "$vm/vmlinuz" -initrd "$vm/initrd.img" \
	-append "root=/dev/vda rw console=ttyS0 systemd.show_status=0 net.ifnames=0" \
	-drive "file=$vm/disk.img,format=raw,if=virtio" \
	-nic none -nographic -no-reboot -serial "file:$log" -monitor none || true

if grep -q "RASBEACON SYSTEM TEST: PASS" "$log"; then
	echo "full-system test passed"
	exit 0
fi
echo "full-system test FAILED; the end of the console:"
tail -n 150 "$log"
exit 1
