import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import AccountProfile from './views/AccountProfile.vue'
import KBaseWorkbench from './views/KBaseWorkbench.vue'
import ModuleLanding from './views/ModuleLanding.vue'

export interface NavigationItem {
  path: string
  label: string
  meta: string
}

export const navigationItems: NavigationItem[] = [
  { path: '/home', label: '首页', meta: 'Discovery' },
  { path: '/course', label: '课程', meta: 'Courses' },
  { path: '/odob', label: '听书书架', meta: 'Audio' },
  { path: '/ebook', label: '电子书架', meta: 'Ebooks' },
  { path: '/knowledge', label: '知识城邦', meta: 'Topics' },
  { path: '/book-knowledge', label: '书籍知识库', meta: 'KBase' },
  { path: '/compass', label: '锦囊', meta: 'Compass' },
  { path: '/setting', label: '设置', meta: 'Admin' },
  { path: '/user/profile', label: '个人中心', meta: 'Account' },
]

const moduleRoutes = [
  {
    path: '/home',
    title: '首页',
    scope: 'home_discovery',
    status: 'planned',
    source: 'frontend/src/views/Home.vue',
    desktopMethods: ['GetHomeInitialState', 'SunflowerLabelList', 'SunflowerLabelContent', 'SunflowerResourceList'],
  },
  {
    path: '/course',
    title: '课程',
    scope: 'course_browser',
    status: 'planned',
    source: 'frontend/src/views/Course.vue',
    desktopMethods: ['CourseCategory', 'CourseList', 'CourseInfo', 'ArticleList', 'ArticleDetail', 'CourseDownload'],
  },
  {
    path: '/odob',
    title: '听书书架',
    scope: 'odob_browser',
    status: 'planned',
    source: 'frontend/src/views/Odob.vue',
    desktopMethods: ['CourseList', 'OdobDownload', 'OdobUserInfo'],
  },
  {
    path: '/ebook',
    title: '电子书架',
    scope: 'ebook_browser',
    status: 'planned',
    source: 'frontend/src/views/Ebook.vue',
    desktopMethods: ['CourseList', 'EbookInfo', 'EbookCommentList', 'EbookDownload', 'EbookDownloadAndSyncWiki'],
  },
  {
    path: '/knowledge',
    title: '知识城邦',
    scope: 'knowledge_city',
    status: 'planned',
    source: 'frontend/src/views/Knowledge.vue',
    desktopMethods: ['TopicAll', 'TopicNoteDetail', 'TopicNotesList'],
  },
  {
    path: '/compass',
    title: '锦囊',
    scope: 'compass_browser',
    status: 'planned',
    source: 'frontend/src/views/Compass.vue',
    desktopMethods: ['CourseCategory', 'CourseList', 'CourseDownload'],
  },
  {
    path: '/setting',
    title: '设置',
    scope: 'server_settings',
    status: 'planned',
    source: 'frontend/src/views/Setting.vue',
    desktopMethods: ['OpenDirectoryDialog', 'SetDir'],
  },
  {
    path: '/user/login',
    title: '登录',
    scope: 'dedao_auth',
    status: 'planned',
    source: 'frontend/src/views/Login.vue',
    desktopMethods: ['GetQrcode', 'CheckLogin'],
  },
  {
    path: '/user/switch',
    title: '切换账号',
    scope: 'account_switch',
    status: 'planned',
    source: 'frontend/src/router/index.ts',
    desktopMethods: ['Logout', 'GetQrcode', 'CheckLogin'],
  },
]

const routes: RouteRecordRaw[] = [
  { path: '/', redirect: '/book-knowledge' },
  {
    path: '/book-knowledge',
    component: KBaseWorkbench,
    meta: {
      title: '书籍知识库',
      scope: 'book_knowledge',
      status: 'online',
      source: 'frontend/src/views/BookKnowledge.vue',
      desktopMethods: ['BookKnowledgeListBooks', 'BookKnowledgeGetBook', 'BookKnowledgeSearch', 'BookKnowledgeChat'],
    },
  },
  {
    path: '/user/profile',
    component: AccountProfile,
    meta: {
      title: '个人中心',
      scope: 'account_profile',
      status: 'online',
      source: 'frontend/src/views/UserCenter.vue',
      desktopMethods: ['UserInfo', 'EbookUserInfo', 'OdobUserInfo'],
    },
  },
  ...moduleRoutes.map((route) => ({
    path: route.path,
    component: ModuleLanding,
    meta: route,
  })),
  { path: '/:pathMatch(.*)*', redirect: '/book-knowledge' },
]

export default createRouter({
  history: createWebHistory(),
  routes,
})
