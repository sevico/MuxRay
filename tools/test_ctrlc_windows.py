import os
import subprocess
import sys
import unittest

from ctrlc_windows import Capture, OwnedJob, Windows, only_owned_console


class CtrlCGuardTests(unittest.TestCase):
    def test_shared_console_is_rejected(self):
        only_owned_console([11, 22], [22, 11])
        for members in ([], [11], [11, 22, 33], [11, 22, 22]):
            with self.assertRaises(ValueError):
                only_owned_console(members, [11, 22])

    def test_capture_is_bounded_and_drains(self):
        options = {"creationflags": subprocess.CREATE_NO_WINDOW} if os.name == "nt" else {}
        process = subprocess.Popen([sys.executable, "-c", "print('hello')"], stdout=subprocess.PIPE, stderr=subprocess.PIPE, **options)
        capture = Capture(process, limit=32)
        self.assertEqual(capture.wait(5), (0, b"hello\r\n" if os.name == "nt" else b"hello\n", b""))
        capture.join()
        process = subprocess.Popen([sys.executable, "-c", "print('x'*1000)"], stdout=subprocess.PIPE, stderr=subprocess.PIPE, **options)
        capture = Capture(process, limit=32)
        try:
            with self.assertRaises(ValueError):
                capture.wait(5)
        finally:
            if process.poll() is None:
                process.kill()
            process.wait(timeout=2)
            capture.join()

    @unittest.skipIf(os.name == "nt", "non-Windows guard")
    def test_no_windows_apis_on_other_systems(self):
        with self.assertRaises(ValueError):
            Windows()

    @unittest.skipUnless(os.name == "nt", "Windows job cleanup")
    def test_job_close_terminates_only_owned_child(self):
        job = OwnedJob(Windows())
        process = subprocess.Popen([sys.executable, "-c", "import time; time.sleep(30)"],
                                   creationflags=subprocess.CREATE_NO_WINDOW)
        try:
            job.assign(process)
            job.close()
            process.wait(timeout=5)
            self.assertIsNotNone(process.returncode)
        finally:
            job.close()
            if process.poll() is None:
                process.kill()
            process.wait(timeout=2)
