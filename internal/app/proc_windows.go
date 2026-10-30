package app

import (
	"os/exec"
	"syscall"
	"unsafe"
)

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObject    = kernel32.NewProc("CreateJobObjectW")
	procSetInformationJob  = kernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJob = kernel32.NewProc("AssignProcessToJobObject")
	jobHandle              uintptr
)

// 本程序退出（包括崩溃）时，Job Object 会让 Windows 自动结束核心进程。
func init() {
	h, _, _ := procCreateJobObject.Call(0, 0)
	if h == 0 {
		return
	}
	// JOBOBJECT_EXTENDED_LIMIT_INFORMATION，只需设置 LimitFlags
	var info struct {
		Basic struct {
			PerProcessUserTimeLimit int64
			PerJobUserTimeLimit     int64
			LimitFlags              uint32
			MinimumWorkingSetSize   uintptr
			MaximumWorkingSetSize   uintptr
			ActiveProcessLimit      uint32
			Affinity                uintptr
			PriorityClass           uint32
			SchedulingClass         uint32
		}
		IoInfo                [6]uint64
		ProcessMemoryLimit    uintptr
		JobMemoryLimit        uintptr
		PeakProcessMemoryUsed uintptr
		PeakJobMemoryUsed     uintptr
	}
	info.Basic.LimitFlags = 0x2000 // JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	r, _, _ := procSetInformationJob.Call(h, 9, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info))
	if r != 0 {
		jobHandle = h
	}
}

func setupChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}

func afterStart(cmd *exec.Cmd) {
	if jobHandle == 0 {
		return
	}
	h, err := syscall.OpenProcess(0x0001|0x0100, false, uint32(cmd.Process.Pid)) // PROCESS_TERMINATE|PROCESS_SET_QUOTA
	if err != nil {
		return
	}
	defer syscall.CloseHandle(h)
	procAssignProcessToJob.Call(jobHandle, uintptr(h))
}

func stopProcess(cmd *exec.Cmd) { cmd.Process.Kill() }

func killStaleCore(Paths) {}

// isElevated 报告当前进程是否以管理员身份运行（TUN 需要）。
func isElevated() bool {
	p := syscall.NewLazyDLL("shell32.dll").NewProc("IsUserAnAdmin")
	if p.Find() != nil {
		return false
	}
	r, _, _ := p.Call()
	return r != 0
}
