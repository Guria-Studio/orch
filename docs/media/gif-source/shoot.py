import sys, time, os
from playwright.sync_api import sync_playwright
out=sys.argv[1]; dur=float(sys.argv[2]); every=float(sys.argv[3])
os.makedirs(out, exist_ok=True)
with sync_playwright() as p:
    b=p.chromium.launch(executable_path="/usr/bin/google-chrome", args=["--no-sandbox"])
    pg=b.new_page(viewport={"width":1440,"height":900})
    pg.goto("http://127.0.0.1:7420/stakeholder/?token=demo-token"); time.sleep(4)
    t0=time.time(); i=0
    while time.time()-t0<dur:
        pg.screenshot(path=f"{out}/f{i:05d}_{int(time.time())}.png"); i+=1; time.sleep(every)
    b.close()
