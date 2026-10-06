"""剪贴板验证摘要与显式同内容发布保护；原内容仅短暂留在验证进程内存。"""
import hashlib


REFRESH_ERRORS = {"refresh_baseline_missing", "refresh_pre_read_unreliable", "refresh_pre_changed",
                  "refresh_publish_failed", "refresh_post_read_unreliable", "refresh_post_changed", "refresh_control_format_unsupported"}
RESCUE_ERRORS = {"rescue_read_unreliable", "rescue_publish_failed", "rescue_post_read_unreliable", "rescue_post_changed", "rescue_control_format_unsupported"}
UTF8_ALIAS = "text/plain;charset=utf-8"
KDE_CONTROL_MIME = "application/x-kde-onlyReplaceEmpty"


def validate_original_formats(summary, baseline, allow_qt_alias=False):
    """核验所有原格式，只允许同内容发布后新增与 plain 摘要完全一致的 Qt 别名。"""
    if summary == baseline:
        return False
    original = {record["mime"]: record for record in baseline["formats"]}
    current = {record["mime"]: record for record in summary["formats"]}
    if len(original) != len(baseline["formats"]) or len(current) != len(summary["formats"]):
        raise RuntimeError("refresh_post_changed")
    if not allow_qt_alias or UTF8_ALIAS in original or "text/plain" not in original or set(current) != set(original) | {UTF8_ALIAS}:
        raise RuntimeError("refresh_post_changed")
    if any(current[mime] != record for mime, record in original.items()):
        raise RuntimeError("refresh_post_changed")
    plain = original["text/plain"]
    if current[UTF8_ALIAS] != dict(plain, mime=UTF8_ALIAS) or summary["total_bytes"] != baseline["total_bytes"] + plain["length"]:
        raise RuntimeError("refresh_post_changed")
    return True


def summarize(formats, read_format, max_bytes=1024 * 1024, max_formats=64):
    if len(formats) > max_formats or len(set(formats)) != len(formats):
        raise RuntimeError("剪贴板格式数量无效或超过验证上限")
    result = []
    total = 0
    for mime in sorted(formats):
        data = read_format(mime)
        size = len(data)
        if size > max_bytes - total:
            raise RuntimeError("剪贴板内容超过验证上限")
        total += size
        result.append({"mime": mime, "length": size, "sha256": hashlib.sha256(bytes(data)).hexdigest()})
    return {"formats": result, "total_bytes": total}


def copy_snapshot(formats, read_format, max_bytes=1024 * 1024, max_formats=64):
    """先检查长度再复制，每个格式的完整字节仅保留在有界内存中。"""
    if len(formats) > max_formats or len(set(formats)) != len(formats):
        raise RuntimeError("剪贴板格式数量无效或超过验证上限")
    contents = {}
    remaining = max_bytes
    for mime in formats:
        data = read_format(mime)
        if len(data) > remaining:
            raise RuntimeError("剪贴板内容超过验证上限")
        remaining -= len(data)
        contents[mime] = bytes(data)
    return summarize(formats, contents.__getitem__, max_bytes, max_formats), contents


def republish_same(read_snapshot, publish, baseline):
    """两次稳定原基线才发布；允许经摘要核验的唯一 Qt 派生别名。"""
    try:
        summary, contents, version = read_snapshot()
        again, _, again_version = read_snapshot()
    except Exception:
        raise RuntimeError("refresh_pre_read_unreliable") from None
    if summary != baseline or again != baseline or version != again_version:
        raise RuntimeError("refresh_pre_changed")
    if KDE_CONTROL_MIME in contents:
        raise RuntimeError("refresh_control_format_unsupported")
    try:
        publish(contents)
    except Exception:
        raise RuntimeError("refresh_publish_failed") from None
    try:
        after, _, _ = read_snapshot()
    except Exception:
        raise RuntimeError("refresh_post_read_unreliable") from None
    validate_original_formats(after, baseline, allow_qt_alias=True)
    return after


def rescue_snapshot(read_snapshot, publish, original, baseline, test_hashes, owns_provider):
    """内容相等不能证明所有者；缺正向所有权证据就保留备份，交由用户恢复。"""
    try:
        current, _, version = read_snapshot()
        again, _, again_version = read_snapshot()
    except Exception:
        raise RuntimeError("rescue_read_unreliable") from None
    if current != again or version != again_version:
        return "needs_user"
    try:
        validate_original_formats(current, baseline, allow_qt_alias=True)
        return "original_preserved"
    except RuntimeError:
        pass
    records = current["formats"]
    texts = [record for record in records if record["mime"] in {"STRING", "TEXT", "UTF8_STRING", "text/plain", UTF8_ALIAS}]
    only_test = bool(texts) and all({"length": record["length"], "sha256": record["sha256"]} in test_hashes for record in texts)
    marker = {"mime": "application/x-kde-onlyReplaceEmpty", "length": 1, "sha256": hashlib.sha256(b"1").hexdigest()}
    only_test = only_test and all(record in texts or record == marker for record in records)
    empty = not records and current["total_bytes"] == 0
    if not (only_test or empty) or not owns_provider():
        return "needs_user"
    if KDE_CONTROL_MIME in original:
        raise RuntimeError("rescue_control_format_unsupported")
    # 再次确认本进程仍拥有提供者；不能以同文本或 KDE 标记猜测外部 owner。
    if not owns_provider():
        return "needs_user"
    try:
        publish(original)
    except Exception:
        raise RuntimeError("rescue_publish_failed") from None
    try:
        after, _, _ = read_snapshot()
    except Exception:
        raise RuntimeError("rescue_post_read_unreliable") from None
    try:
        validate_original_formats(after, baseline, allow_qt_alias=True)
    except Exception:
        raise RuntimeError("rescue_post_changed") from None
    return "rescued"
