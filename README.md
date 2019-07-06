# OpenRisk
Open source post-trade risk management system

[Demo](http://demo.opentradesolutions.com/#/risk)

# Features

- Highly integrated with OpenTrade
- Flexible formula support
- Python function call support in formula
- Graph support on GUI
- Live risk editor on GUI

---

[![OpenRisk](https://github.com/opentradesolutions/openrisk/blob/master/screencapture.png)](https://raw.githubusercontent.com/opentradesolutions/openrisk/master/screencapture.png)

---

# Steps to run

Make sure opentrade is running, openrisk connects to opentrade and share its frontend
```bash
go get github.com/opentradesolutions/openrisk
cd .gopath/src/github.com/opentradesolutions/openrisk
make run
```
Now, you can open "http://localhost:9111/#/risk" on your browser.
