"""验证冒烟脚本在启动失败或服务不退出时仍有时间边界。"""
import os
import subprocess
import sys
import time
import unittest

from smoke import read_startup, stop_service


class LifecycleTest(unittest.TestCase):
    def start(self, code):
        service = subprocess.Popen(
            [sys.executable, "-c", code], stdout=subprocess.PIPE, text=True,
        )
        self.addCleanup(lambda: stop_service(service, timeout=0.2))
        return service

    def test_startup_timeout(self):
        service = self.start("import time; time.sleep(60)")
        start = time.monotonic()
        with self.assertRaises(TimeoutError):
            read_startup(service, timeout=0.1)
        self.assertLess(time.monotonic() - start, 2)
        stop_service(service, timeout=0.2)
        self.assertIsNotNone(service.poll())

    def test_exited_service(self):
        service = self.start("raise SystemExit(3)")
        service.wait(timeout=5)
        stop_service(service, timeout=0.2)
        self.assertEqual(service.returncode, 3)

    @unittest.skipIf(os.name == "nt", "Windows terminate 为强制结束，无 SIGTERM 忽略路径")
    def test_force_stop(self):
        service = self.start(
            "import signal, time; signal.signal(signal.SIGTERM, signal.SIG_IGN); "
            "print('{}', flush=True); time.sleep(60)"
        )
        self.assertEqual(read_startup(service), {})
        start = time.monotonic()
        stop_service(service, timeout=0.1)
        self.assertLess(time.monotonic() - start, 2)
        self.assertIsNotNone(service.poll())


if __name__ == "__main__":
    unittest.main()
