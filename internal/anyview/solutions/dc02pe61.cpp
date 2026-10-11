#include "allinclude.h"  //DO NOT edit this line

Status QueueEmpty_LQ(LQueue Q)
{
    // 不带头结点的链队列: 队空当且仅当头、尾指针皆为 NULL (同 DC02PE53 链栈惯例)
    if (Q.front == Q.rear && Q.front == NULL) {
        return TRUE;
    } else {
        return FALSE;
    }
}
