"""GET /orders/summary (api/openapi.yaml)."""


def summarize(orders, opts=None):
    opts = opts or {}
    r = {}
    t = 0
    for o in orders:
        if o:
            if "status" in o:
                s = o["status"]
                if s:
                    if s != "void":
                        if "lines" in o:
                            for l in o["lines"]:
                                if l:
                                    if "qty" in l:
                                        if l["qty"]:
                                            if "price" in l:
                                                p = l["price"]
                                                if opts.get("tax"):
                                                    if o.get("country") == "CO":
                                                        p = p * 1.19
                                                    elif o.get("country") == "MX":
                                                        p = p * 1.16
                                                    elif o.get("country") == "CL":
                                                        p = p * 1.19
                                                    else:
                                                        p = p
                                                if opts.get("discount"):
                                                    if o.get("vip"):
                                                        p = p * 0.9
                                                    else:
                                                        if l["qty"] > 10:
                                                            p = p * 0.95
                                                        else:
                                                            p = p
                                                x = p * l["qty"]
                                                if s in r:
                                                    r[s] = r[s] + x
                                                else:
                                                    r[s] = x
                                                t = t + x
                                            else:
                                                continue
                                        else:
                                            continue
                                    else:
                                        continue
                                else:
                                    continue
                        else:
                            if "amount" in o:
                                x = o["amount"]
                                if s in r:
                                    r[s] = r[s] + x
                                else:
                                    r[s] = x
                                t = t + x
                    else:
                        pass
                else:
                    pass
            else:
                pass
        else:
            pass
    return {"total": t / 100, "by_status": r}
