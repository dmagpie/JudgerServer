package container

import (
	"fmt"
	"io/ioutil"
	"os"
	"syscall"
	"unsafe"

	seccomp "github.com/seccomp/libseccomp-golang"
)

/*
#include <unistd.h>
#include <sched.h>
#include <stdio.h>

void run_child(char *path, char *args, char *envs) {
	execve(path, args, envs);
}

*/
import "C"

// RunnerConfig CreateRunner的配置项
type RunnerConfig struct {
	WorkDir        string
	ChangeRoot     bool
	GID            int
	UID            int
	Arguments      []string
	Envirment      []string
	RunablePath    string
	OutputPath     string
	InputPath      string
	ErrorPath      string
	SeccompRule    []seccomp.ScmpSyscall
	SeccompType    seccomp.ScmpAction
	RestrictExecve bool // g++会调用execve...
	// 资源限制
	// 0 < UNLIMITED
	MemoryLimit        int64  // Byte
	CPUTimeLimit       int64  // ms
	ProcessNumberLimit int64  // 个 最大创建进程数限制
	OutputSizeLimit    int64  // Byte 最大的输出文件大小
	CoreDumpLimit      uint64 // Byte 最大的核心转储大小 为0则禁用
	StackLimit         int64  // Byte 栈大小限制
}

// DefaultSeccompBlacklist 默认黑名单
var DefaultSeccompBlacklist []seccomp.ScmpSyscall

func init() {
	DefaultSeccompBlacklist = []seccomp.ScmpSyscall{
		GetSyscallNumber("acct"),
		GetSyscallNumber("add_key"),
		GetSyscallNumber("bpf"),
		GetSyscallNumber("clock_adjtime"),
		GetSyscallNumber("clock_settime"),
		GetSyscallNumber("clone"),
		GetSyscallNumber("chroot"),
		GetSyscallNumber("chdir"),
		GetSyscallNumber("create_module"),
		GetSyscallNumber("delete_module"),
		GetSyscallNumber("execveat"),
		GetSyscallNumber("finit_module"),
		GetSyscallNumber("get_kernel_syms"),
		GetSyscallNumber("get_mempolicy"),
		GetSyscallNumber("init_module"),
		GetSyscallNumber("ioperm"),
		GetSyscallNumber("iopl"),
		GetSyscallNumber("kcmp"),
		GetSyscallNumber("kexec_file_load"),
		GetSyscallNumber("kexec_load"),
		GetSyscallNumber("keyctl"),
		GetSyscallNumber("lookup_dcookie"),
		GetSyscallNumber("mbind"),
		GetSyscallNumber("mount"),
		GetSyscallNumber("move_pages"),
		GetSyscallNumber("name_to_handle_at"),
		GetSyscallNumber("nfsservctl"),
		GetSyscallNumber("open_by_handle_at"),
		GetSyscallNumber("perf_event_open"),
		GetSyscallNumber("personality"),
		GetSyscallNumber("pivot_root"),
		GetSyscallNumber("process_vm_readv"),
		GetSyscallNumber("process_vm_writev"),
		GetSyscallNumber("ptrace"),
		GetSyscallNumber("query_module"),
		GetSyscallNumber("quotactl"),
		GetSyscallNumber("reboot"),
		GetSyscallNumber("request_key"),
		GetSyscallNumber("set_mempolicy"),
		GetSyscallNumber("setns"),
		GetSyscallNumber("settimeofday"),
		GetSyscallNumber("setrlimit"),
		GetSyscallNumber("stime"),
		GetSyscallNumber("swapon"),
		GetSyscallNumber("swapoff"),
		GetSyscallNumber("sysfs"),
		GetSyscallNumber("_sysctl"),
		GetSyscallNumber("umount"),
		GetSyscallNumber("umount2"),
		GetSyscallNumber("unshare"),
		GetSyscallNumber("uselib"),
		GetSyscallNumber("userfaultfd"),
		GetSyscallNumber("ustat"),
		GetSyscallNumber("vm86"),
		GetSyscallNumber("vm86old"),
	}
}

// MapUser 映射命名空间内外的用户
// 不知道为啥会Operation not permitted
func MapUser(uid, gid, pid int) {
	ufile := fmt.Sprintf("/proc/%d/uid_map", pid)
	udata := []byte(fmt.Sprintf("%d %d %d", 0, uid, 0))
	if err := ioutil.WriteFile(ufile, udata, 0777); err != nil {
		panic(err)
	}

	gfile := fmt.Sprintf("/proc/%d/gid_map", pid)
	gdata := []byte(fmt.Sprintf("%d %d %d", 0, gid, 0))
	if err := ioutil.WriteFile(gfile, gdata, 0777); err != nil {
		panic(err)
	}
}

// If 假装有三目运算符
func If(b bool, t, f interface{}) interface{} {
	if b {
		return t
	}
	return f
}

// CreateRunner 创建运行进程
func CreateRunner(config *RunnerConfig) {
	// 初始化通讯管道
	pipefd := make([]int, 2)
	syscall.Pipe(pipefd)

	if err := syscall.Unshare(syscall.CLONE_NEWPID); err != nil {
		panic(err)
	}

	pid := Fork()
	if pid == 0 {
		// 子进程
		// 等待父进程
		if err := syscall.Close(pipefd[1]); err != nil {
			panic(err)
		}
		if _, err := syscall.Read(pipefd[0], []byte{1}); err != nil {
			panic(err)
		}

		// 切换工作目录
		if err := syscall.Chdir(config.WorkDir); err != nil {
			panic(err)
		}

		// 隔离命名空间
		if err := syscall.Unshare(syscall.CLONE_NEWUTS | syscall.CLONE_NEWIPC | syscall.CLONE_NEWPID | syscall.CLONE_NEWNS | syscall.CLONE_NEWUSER | syscall.CLONE_FILES | syscall.CLONE_FS); err != nil {
			panic(err)
		}

		// 创建文件夹
		if err := os.MkdirAll(config.WorkDir+"/proc", 0777); err != nil {
			panic(err)
		}
		if err := os.MkdirAll(config.WorkDir+"/dev", 0777); err != nil {
			panic(err)
		}
		if err := os.MkdirAll(config.WorkDir+"/bin", 0777); err != nil {
			panic(err)
		}
		if err := os.MkdirAll(config.WorkDir+"/lib", 0777); err != nil {
			panic(err)
		}
		if err := os.MkdirAll(config.WorkDir+"/lib64", 0777); err != nil {
			panic(err)
		}
		if err := os.MkdirAll(config.WorkDir+"/usr/lib", 0777); err != nil {
			panic(err)
		}
		if err := os.MkdirAll(config.WorkDir+"/usr/bin", 0777); err != nil {
			panic(err)
		}

		// 重新挂载部分文件系统
		if err := syscall.Mount("proc", "/proc", "proc", syscall.MS_PRIVATE, ""); err != nil {
			panic(err)
		}
		if err := syscall.Mount("udev", "/dev", "devtmpfs", syscall.MS_PRIVATE, ""); err != nil {
			panic(err)
		}

		// chroot jail
		if config.ChangeRoot {
			// 绑定挂载部分文件夹
			if err := syscall.Mount("/usr/lib", config.WorkDir+"/usr/lib", "none", syscall.MS_BIND, ""); err != nil {
				panic(err)
			}
			if err := syscall.Mount("/lib", config.WorkDir+"/lib", "none", syscall.MS_BIND, ""); err != nil {
				panic(err)
			}
			if err := syscall.Mount("/lib64", config.WorkDir+"/lib64", "none", syscall.MS_BIND, ""); err != nil {
				panic(err)
			}
			if err := syscall.Mount("/bin", config.WorkDir+"/bin", "none", syscall.MS_BIND, ""); err != nil {
				panic(err)
			}
			if err := syscall.Mount("/usr/bin", config.WorkDir+"/usr/bin", "none", syscall.MS_BIND, ""); err != nil {
				panic(err)
			}

			if err := syscall.Chroot("./"); err != nil {
				panic(err)
			}
		}

		// 重定向IO流
		inputfd, err := syscall.Open(config.InputPath, syscall.O_RDONLY, 0666)
		if err != nil {
			panic(err)
		}
		outputfd, err := syscall.Open(config.OutputPath, syscall.O_WRONLY, 0666)
		if err != nil {
			panic(err)
		}
		errorfd, err := syscall.Open(config.ErrorPath, syscall.O_WRONLY, 0666)
		if err != nil {
			panic(err)
		}
		if err := syscall.Dup2(inputfd, int(os.Stdin.Fd())); err != nil {
			panic(err)
		}
		if err := syscall.Dup2(outputfd, int(os.Stdout.Fd())); err != nil {
			panic(err)
		}
		if err := syscall.Dup2(errorfd, int(os.Stderr.Fd())); err != nil {
			panic(err)
		}

		// 设置资源限制
		if config.MemoryLimit > 0 {
			if err := syscall.Setrlimit(syscall.RLIMIT_AS, &syscall.Rlimit{
				Cur: uint64(config.MemoryLimit * 2),
				Max: uint64(config.MemoryLimit * 2),
			}); err != nil {
				panic(err)
			}
		}
		if config.CPUTimeLimit > 0 {
			// CPU时间额外给出1秒
			if err := syscall.Setrlimit(syscall.RLIMIT_CPU, &syscall.Rlimit{
				Cur: uint64(config.CPUTimeLimit+1000) / 1000,
				Max: uint64(config.CPUTimeLimit+1000) / 1000,
			}); err != nil {
				panic(err)
			}
		}
		if config.OutputSizeLimit > 0 {
			if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{
				Cur: uint64(config.OutputSizeLimit),
				Max: uint64(config.OutputSizeLimit),
			}); err != nil {
				panic(err)
			}
		}
		if config.StackLimit > 0 {
			if err := syscall.Setrlimit(syscall.RLIMIT_STACK, &syscall.Rlimit{
				Cur: uint64(config.StackLimit),
				Max: uint64(config.StackLimit),
			}); err != nil {
				panic(err)
			}
		}
		if err := syscall.Setrlimit(syscall.RLIMIT_CORE, &syscall.Rlimit{
			Cur: uint64(config.CoreDumpLimit),
			Max: uint64(config.CoreDumpLimit),
		}); err != nil {
			panic(err)
		}

		// Seccomp 规则
		filter, err := seccomp.NewFilter(config.SeccompType)
		if err != nil {
			panic(err)
		}
		for _, s := range config.SeccompRule {
			if config.SeccompType == seccomp.ActAllow {
				filter.AddRule(s, seccomp.ActKill)
			} else {
				filter.AddRule(s, seccomp.ActAllow)
			}
		}
		targetPath := C.CString(config.RunablePath)
		if config.RestrictExecve {
			if config.SeccompType == seccomp.ActKill {
				execveAllow, err := seccomp.MakeCondition(0, seccomp.CompareEqual, uint64((uintptr)(unsafe.Pointer(targetPath))))
				if err != nil {
					panic(err)
				}
				if err := filter.AddRuleConditional(GetSyscallNumber("execve"), seccomp.ActAllow, []seccomp.ScmpCondition{execveAllow}); err != nil {
					panic(err)
				}
			} else {
				execveDeny, err := seccomp.MakeCondition(0, seccomp.CompareNotEqual, uint64((uintptr)(unsafe.Pointer(targetPath))))
				if err != nil {
					panic(err)
				}
				if err := filter.AddRuleConditional(GetSyscallNumber("execve"), seccomp.ActKill, []seccomp.ScmpCondition{execveDeny}); err != nil {
					panic(err)
				}
			}
		}
		if err := filter.Load(); err != nil {
			filter.Release()
			panic(err)
		}
		filter.Release()

		// EXECVE子进程
		args, err := syscall.SlicePtrFromStrings(config.Arguments)
		if err != nil {
			panic(err)
		}
		envs, err := syscall.SlicePtrFromStrings(config.Envirment)
		if err != nil {
			panic(err)
		}
		C.run_child(targetPath, (*C.char)(unsafe.Pointer(&args[0])), (*C.char)(unsafe.Pointer(&envs[0])))

		// 子进程execve失败 退出
		os.Exit(10)
	} else if pid > 0 {
		// 父进程
		// MapUser(config.UID, config.GID, pid)
		// 通知子进程
		if err := syscall.Close(pipefd[1]); err != nil {
			panic(err)
		}

		// 等待子进程
		wstatus := new(syscall.WaitStatus)
		rusage := syscall.Rusage{}
		wpid, err := syscall.Wait4(pid, wstatus, 0, &rusage)

		if err != nil {
			panic(err)
		}

		fmt.Printf("子进程退出：%d 状态：%d 信号: %d %s\n", wpid, wstatus.ExitStatus(), wstatus.Signal(), wstatus.Signal().String())

		return
	} else {
		// 运行错误
		panic("fork failed")
	}
}
