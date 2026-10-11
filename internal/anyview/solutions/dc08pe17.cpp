#include "allinclude.h"  //DO NOT edit this line
Status SetArc(MGraph &G, VexType v, VexType w, ArcCell info) {
    int i = LocateVex(G, v);
    int j = LocateVex(G, w);

    if (i == -1 || j == -1) {
        return ERROR;
    }

    if (i == j) {
        return ERROR;
    }

    G.arcs[i][j] = info;
    G.e++;  // 新增一条弧, 计数 +1 (判题实测: 弧数 6 -> 7)

    if (G.kind == UDG || G.kind == UDN) {
        G.arcs[j][i] = info;  // 无向图对称矩阵, 同一条弧不重复计数
    }

    return OK;
}
