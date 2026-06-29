import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import AccountLogin from './views/AccountLogin.vue'
import AccountProfile from './views/AccountProfile.vue'
import CompassLibrary from './views/CompassLibrary.vue'
import CourseDetailReader from './views/CourseDetailReader.vue'
import CourseLibrary from './views/CourseLibrary.vue'
import EbookDetailReader from './views/EbookDetailReader.vue'
import EbookLibrary from './views/EbookLibrary.vue'
import HomeDiscovery from './views/HomeDiscovery.vue'
import KBaseWorkbench from './views/KBaseWorkbench.vue'
import KnowledgeCity from './views/KnowledgeCity.vue'
import ModuleLanding from './views/ModuleLanding.vue'
import OdobLibrary from './views/OdobLibrary.vue'
import WebSettings from './views/WebSettings.vue'

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
    path: '/home',
    component: HomeDiscovery,
    meta: {
      title: '首页',
      scope: 'home_discovery',
      status: 'online',
      source: 'frontend/src/views/Home.vue',
      desktopMethods: ['GetHomeInitialState', 'SunflowerLabelList', 'SunflowerLabelContent', 'SunflowerResourceList'],
    },
  },
  {
    path: '/book-knowledge',
    component: KBaseWorkbench,
    meta: {
      title: '书籍知识库',
      scope: 'book_knowledge',
      status: 'online',
      wide: true,
      source: 'frontend/src/views/BookKnowledge.vue',
      desktopMethods: ['BookKnowledgeListBooks', 'BookKnowledgeGetBook', 'BookKnowledgeSearch', 'BookKnowledgeChat'],
    },
  },
  {
    path: '/knowledge',
    component: KnowledgeCity,
    meta: {
      title: '知识城邦',
      scope: 'knowledge_city',
      status: 'online',
      source: 'frontend/src/views/Knowledge.vue',
      desktopMethods: ['TopicAll', 'TopicNoteDetail', 'TopicNotesList'],
    },
  },
  {
    path: '/compass',
    component: CompassLibrary,
    meta: {
      title: '锦囊',
      scope: 'compass_browser',
      status: 'online',
      source: 'frontend/src/views/Compass.vue',
      desktopMethods: ['CourseCategory', 'CourseList', 'CourseDownload'],
    },
  },
  {
    path: '/user/login',
    component: AccountLogin,
    meta: {
      title: '登录',
      scope: 'dedao_auth',
      status: 'online',
      source: 'frontend/src/views/Login.vue',
      desktopMethods: ['GetQrcode', 'CheckLogin'],
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
  {
    path: '/course/:enid',
    component: CourseDetailReader,
    meta: {
      title: '课程阅读',
      scope: 'course_detail_reader',
      status: 'online',
      source: 'frontend/src/views/Course.vue',
      desktopMethods: ['CourseInfo', 'ArticleList', 'ArticleDetail'],
    },
  },
  {
    path: '/course',
    component: CourseLibrary,
    meta: {
      title: '课程',
      scope: 'course_browser',
      status: 'online',
      source: 'frontend/src/views/Course.vue',
      desktopMethods: ['CourseCategory', 'CourseList', 'CourseInfo', 'ArticleList', 'ArticleDetail', 'CourseDownload'],
    },
  },
  {
    path: '/ebook/:enid',
    component: EbookDetailReader,
    meta: {
      title: '电子书阅读',
      scope: 'ebook_detail_reader',
      status: 'online',
      immersive: true,
      source: 'frontend/src/views/Ebook.vue',
      desktopMethods: ['EbookDetail', 'EbookInfo', 'EbookPage'],
    },
  },
  {
    path: '/ebook',
    component: EbookLibrary,
    meta: {
      title: '电子书架',
      scope: 'ebook_browser',
      status: 'online',
      source: 'frontend/src/views/Ebook.vue',
      desktopMethods: ['CourseList', 'EbookInfo', 'EbookCommentList', 'EbookDownload', 'EbookDownloadAndSyncWiki'],
    },
  },
  {
    path: '/odob',
    component: OdobLibrary,
    meta: {
      title: '听书书架',
      scope: 'odob_browser',
      status: 'online',
      source: 'frontend/src/views/Odob.vue',
      desktopMethods: ['CourseList', 'OdobDownload', 'OdobUserInfo'],
    },
  },
  {
    path: '/setting',
    component: WebSettings,
    meta: {
      title: '设置',
      scope: 'server_settings',
      status: 'online',
      source: 'frontend/src/views/Setting.vue',
      desktopMethods: ['OpenDirectoryDialog', 'SetDir'],
    },
  },
  ...moduleRoutes.map((route) => ({
    path: route.path,
    component: ModuleLanding,
    meta: route,
  })).filter((route) => !['/setting', '/home', '/knowledge', '/compass'].includes(route.path)),
  { path: '/:pathMatch(.*)*', redirect: '/book-knowledge' },
]

export default createRouter({
  history: createWebHistory(),
  routes,
})
