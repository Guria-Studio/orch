"""Stitch the per-cut casts into one asciicast v2, compressing time uniformly
per segment (never dropping events). Cut 3 (the `orch run` output) is
compressed to a fixed duration; the other segments get a mild speed-up and an
idle cap. Prints the timeline so GIF-NOTES.md can quote it."""
import json, sys

def load(p):
    lines = open(p).read().splitlines()
    return json.loads(lines[0]), [json.loads(l) for l in lines[1:] if l.strip()]

def outputs(evs):
    return [e for e in evs if e[1] == "o"]

def retime(evs, speed=1.0, idle=None, start=0.0):
    out, prev_src, t = [], None, start
    for ts, kind, data in evs:
        gap = 0.0 if prev_src is None else ts - prev_src
        if idle is not None:
            gap = min(gap, idle)
        t += gap / speed
        out.append([round(t, 4), kind, data])
        prev_src = ts
    return out, t

hdr, c1 = load("cut1.v2.cast")
_, c23 = load("cut23.v2.cast")
_, c4 = load("cut4.v2.cast")
c1, c23, c4 = outputs(c1), outputs(c23), outputs(c4)

# split cut23 right after the typed "orch run" line ends (the "\r\n" after "n")
typed = ""
split = None
for i, e in enumerate(c23):
    typed += e[2]
    if typed.endswith("$ orch run\r\n") or typed.endswith("$ orch run\n"):
        split = i + 1
        break
assert split, "could not find the typed `orch run`"
c2, c3 = c23[:split], c23[split:]

RUN_SECONDS = float(sys.argv[1]) if len(sys.argv) > 1 else 9.0
timeline, t = [], 0.0
seg, t_end = retime(c1, speed=1.4, idle=1.0, start=t); timeline.append(("cut1 orch init", t, t_end, "1.4x, gaps capped at 1 s")); t = t_end + 1.0
all_ev = seg
seg, t_end = retime(c2, speed=1.4, idle=1.0, start=t); timeline.append(("cut2 orch tasks / estimate", t, t_end, "1.4x, gaps capped at 1 s")); all_ev += seg; t = t_end
real = c3[-1][0] - c3[0][0]
seg, t_end = retime(c3, speed=real / RUN_SECONDS, start=t); timeline.append(("cut3 orch run", t, t_end, f"{real:.0f} s real -> {RUN_SECONDS:.0f} s, uniform {real / RUN_SECONDS:.1f}x")); all_ev += seg; t = t_end + 1.5
seg, t_end = retime(c4, speed=1.4, idle=0.8, start=t); timeline.append(("cut4 gh pr list / checks", t, t_end, "1.4x, gaps capped at 0.8 s")); all_ev += seg; t = t_end

hdr = {"version": 2, "width": 100, "height": 30}
with open("terminal.cast", "w") as f:
    f.write(json.dumps(hdr) + "\n")
    for e in all_ev:
        f.write(json.dumps(e) + "\n")
for name, a, b, how in timeline:
    print(f"{name:28s} {a:6.2f} -> {b:6.2f}  ({how})")
print(f"terminal total {t:.2f} s")
